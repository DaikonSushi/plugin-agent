package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DaikonSushi/bot-platform/pkg/pluginsdk"
)

type ToolRegistry struct {
	cfg       *Config
	state     *State
	bot       *pluginsdk.BotClient
	scheduler *Scheduler
	indexer   *Indexer
	skills    *SkillStore
}

type ToolResult struct {
	Content string
	Error   string
}

func NewToolRegistry(cfg *Config, state *State, bot *pluginsdk.BotClient) *ToolRegistry {
	skills := NewSkillStore(cfg.SkillsDir)
	_ = skills.Load()
	tr := &ToolRegistry{cfg: cfg, state: state, bot: bot, skills: skills}
	tr.indexer = NewIndexer(cfg, state)
	return tr
}

func (r *ToolRegistry) Definitions() []ToolDef {
	return []ToolDef{
		toolDef("list_files", "List files under an allowed path.", map[string]any{"path": strProp(), "max": intProp()}),
		toolDef("read_file", "Read a text file under an allowed path.", map[string]any{"path": strProp(), "max_bytes": intProp()}),
		toolDef("search_code", "Search allowed projects with ripgrep.", map[string]any{"query": strProp(), "path": strProp(), "max": intProp()}),
		toolDef("run_command", "Run an allowlisted shell command in an allowed working directory.", map[string]any{"command": strProp(), "workdir": strProp(), "timeout_seconds": intProp()}),
		toolDef("ssh_command", "Run an allowlisted command on an allowlisted Tailscale/SSH host.", map[string]any{"host": strProp(), "command": strProp(), "timeout_seconds": intProp()}),
		toolDef("http_request", "Call an allowlisted HTTP endpoint.", map[string]any{"method": strProp(), "url": strProp(), "body": strProp()}),
		toolDef("send_qq_message", "Send a QQ private or group message.", map[string]any{"target_type": strProp(), "target_id": intProp(), "message": strProp()}),
		toolDef("schedule_task", "Create a reminder or recurring agent task.", map[string]any{"name": strProp(), "prompt": strProp(), "target_type": strProp(), "target_id": intProp(), "schedule": strProp()}),
		toolDef("project_index", "Build or read the local project index.", map[string]any{"action": strProp()}),
		toolDef("load_skill", "Load skill content by skill name.", map[string]any{"name": strProp()}),
	}
}

func toolDef(name, desc string, props map[string]any) ToolDef {
	return ToolDef{Type: "function", Function: ToolDefFunction{
		Name:        name,
		Description: desc,
		Parameters: map[string]any{
			"type":       "object",
			"properties": props,
		},
	}}
}

func strProp() map[string]any { return map[string]any{"type": "string"} }
func intProp() map[string]any { return map[string]any{"type": "integer"} }

func (r *ToolRegistry) Execute(ctx context.Context, name string, raw json.RawMessage) ToolResult {
	input := string(raw)
	var out ToolResult
	if !r.cfg.Permission.AutoExecute && name != "project_index" && name != "load_skill" {
		out = ToolResult{Error: "tool auto execution is disabled"}
		r.state.AddAudit(name, input, "error", out.Error)
		return out
	}
	switch name {
	case "list_files":
		out = r.listFiles(raw)
	case "read_file":
		out = r.readFile(raw)
	case "search_code":
		out = r.searchCode(ctx, raw)
	case "run_command":
		out = r.runCommand(ctx, raw)
	case "ssh_command":
		out = r.sshCommand(ctx, raw)
	case "http_request":
		out = r.httpRequest(ctx, raw)
	case "send_qq_message":
		out = r.sendQQ(raw)
	case "schedule_task":
		out = r.scheduleTask(raw)
	case "project_index":
		out = r.projectIndex(raw)
	case "load_skill":
		out = r.loadSkill(raw)
	default:
		out = ToolResult{Error: "unknown tool: " + name}
	}
	status := "ok"
	summary := out.Content
	if out.Error != "" {
		status = "error"
		summary = out.Error
	}
	r.state.AddAudit(name, input, status, shortText(summary, 500))
	return out
}

func (r *ToolRegistry) listFiles(raw json.RawMessage) ToolResult {
	var args struct {
		Path string `json:"path"`
		Max  int    `json:"max"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Path == "" {
		args.Path = "."
	}
	if args.Max <= 0 || args.Max > 500 {
		args.Max = 100
	}
	path, err := r.allowedPath(args.Path)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	var lines []string
	for i, ent := range entries {
		if i >= args.Max {
			break
		}
		suffix := ""
		if ent.IsDir() {
			suffix = "/"
		}
		lines = append(lines, ent.Name()+suffix)
	}
	return ToolResult{Content: strings.Join(lines, "\n")}
}

func (r *ToolRegistry) readFile(raw json.RawMessage) ToolResult {
	var args struct {
		Path     string `json:"path"`
		MaxBytes int    `json:"max_bytes"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.MaxBytes <= 0 || args.MaxBytes > 200000 {
		args.MaxBytes = 50000
	}
	path, err := r.allowedPath(args.Path)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	f, err := os.Open(path)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(args.MaxBytes)))
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	return ToolResult{Content: string(data)}
}

func (r *ToolRegistry) searchCode(ctx context.Context, raw json.RawMessage) ToolResult {
	var args struct {
		Query string `json:"query"`
		Path  string `json:"path"`
		Max   int    `json:"max"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Query == "" {
		return ToolResult{Error: "query is required"}
	}
	if args.Path == "" {
		args.Path = "."
	}
	path, err := r.allowedPath(args.Path)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	if args.Max <= 0 || args.Max > 200 {
		args.Max = 80
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "rg", "-n", "--no-heading", "--color", "never", args.Query, path)
	data, err := cmd.CombinedOutput()
	if err != nil && len(data) == 0 {
		return ToolResult{Error: err.Error()}
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > args.Max {
		lines = lines[:args.Max]
	}
	return ToolResult{Content: strings.Join(lines, "\n")}
}

func (r *ToolRegistry) runCommand(ctx context.Context, raw json.RawMessage) ToolResult {
	var args struct {
		Command        string `json:"command"`
		Workdir        string `json:"workdir"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	_ = json.Unmarshal(raw, &args)
	if err := r.allowedCommand(args.Command); err != nil {
		return ToolResult{Error: err.Error()}
	}
	if args.Workdir == "" {
		args.Workdir = "."
	}
	workdir, err := r.allowedPath(args.Workdir)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	if args.TimeoutSeconds <= 0 || args.TimeoutSeconds > 120 {
		args.TimeoutSeconds = 30
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(args.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "bash", "-lc", args.Command)
	cmd.Dir = workdir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err = cmd.Run()
	out := shortText(buf.String(), 12000)
	if err != nil {
		return ToolResult{Content: out, Error: err.Error() + "\n" + out}
	}
	return ToolResult{Content: out}
}

func (r *ToolRegistry) sshCommand(ctx context.Context, raw json.RawMessage) ToolResult {
	var args struct {
		Host           string `json:"host"`
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	_ = json.Unmarshal(raw, &args)
	if err := r.allowedHost(args.Host, r.cfg.Permission.AllowedHosts); err != nil {
		return ToolResult{Error: err.Error()}
	}
	if err := r.allowedCommand(args.Command); err != nil {
		return ToolResult{Error: err.Error()}
	}
	if args.TimeoutSeconds <= 0 || args.TimeoutSeconds > 120 {
		args.TimeoutSeconds = 30
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(args.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", args.Host, args.Command)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := shortText(buf.String(), 12000)
	if err != nil {
		return ToolResult{Content: out, Error: err.Error() + "\n" + out}
	}
	return ToolResult{Content: out}
}

func (r *ToolRegistry) httpRequest(ctx context.Context, raw json.RawMessage) ToolResult {
	var args struct {
		Method string `json:"method"`
		URL    string `json:"url"`
		Body   string `json:"body"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Method == "" {
		args.Method = http.MethodGet
	}
	u, err := url.Parse(args.URL)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	if err := r.allowedHost(u.Hostname(), r.cfg.Permission.AllowedHTTPHosts); err != nil {
		return ToolResult{Error: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, args.Method, args.URL, strings.NewReader(args.Body))
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return ToolResult{Content: fmt.Sprintf("HTTP %s\n%s", resp.Status, string(data))}
}

func (r *ToolRegistry) sendQQ(raw json.RawMessage) ToolResult {
	var args struct {
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		Message    string `json:"message"`
	}
	_ = json.Unmarshal(raw, &args)
	var err error
	switch args.TargetType {
	case "group":
		_, err = r.bot.SendGroupMessage(args.TargetID, pluginsdk.Text(args.Message))
	case "private":
		_, err = r.bot.SendPrivateMessage(args.TargetID, pluginsdk.Text(args.Message))
	default:
		return ToolResult{Error: "target_type must be group or private"}
	}
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	return ToolResult{Content: "sent"}
}

func (r *ToolRegistry) scheduleTask(raw json.RawMessage) ToolResult {
	if r.scheduler == nil {
		return ToolResult{Error: "scheduler is not ready"}
	}
	var args struct {
		Name       string `json:"name"`
		Prompt     string `json:"prompt"`
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		Schedule   string `json:"schedule"`
	}
	_ = json.Unmarshal(raw, &args)
	task, err := r.scheduler.Add(args.Name, args.Prompt, args.TargetType, args.TargetID, args.Schedule)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	return ToolResult{Content: "scheduled task " + task.ID + " next run " + task.NextRunAt.Format(time.RFC3339)}
}

func (r *ToolRegistry) projectIndex(raw json.RawMessage) ToolResult {
	var args struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Action == "rebuild" || args.Action == "" {
		if err := r.indexer.Rebuild(); err != nil {
			return ToolResult{Error: err.Error()}
		}
	}
	text, err := r.indexer.Read()
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	return ToolResult{Content: text}
}

func (r *ToolRegistry) loadSkill(raw json.RawMessage) ToolResult {
	var args struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &args)
	s, ok := r.skills.Get(args.Name)
	if !ok {
		return ToolResult{Error: "skill not found"}
	}
	return ToolResult{Content: s.Content}
}

func (r *ToolRegistry) allowedPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	for _, base := range r.cfg.Permission.AllowedPaths {
		baseAbs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(baseAbs, abs)
		if err == nil && (rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("path is outside allowed_paths: %s", p)
}

func (r *ToolRegistry) allowedCommand(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("command is required")
	}
	for _, part := range r.cfg.Permission.DeniedCommandParts {
		if part != "" && strings.Contains(command, part) {
			return fmt.Errorf("command denied by denylist")
		}
	}
	for _, prefix := range r.cfg.Permission.AllowedCommandPref {
		if strings.HasPrefix(command, prefix) {
			return nil
		}
	}
	return fmt.Errorf("command is not allowlisted: %s", command)
}

func (r *ToolRegistry) allowedHost(host string, allowed []string) error {
	if host == "" {
		return fmt.Errorf("host is required")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		for _, item := range allowed {
			if item == host {
				return nil
			}
		}
	}
	for _, item := range allowed {
		if item == host || strings.HasSuffix(host, "."+item) {
			return nil
		}
	}
	return fmt.Errorf("host is not allowlisted: %s", host)
}
