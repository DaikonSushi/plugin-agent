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
		toolDef("write_file", "Create or overwrite a text file under an allowed path.", map[string]any{"path": strProp(), "content": strProp()}),
		toolDef("search_code", "Search allowed projects with ripgrep.", map[string]any{"query": strProp(), "path": strProp(), "max": intProp()}),
		toolDef("run_command", "Run an allowlisted shell command in an allowed working directory.", map[string]any{"command": strProp(), "workdir": strProp(), "timeout_seconds": intProp()}),
		toolDef("ssh_command", "Run an allowlisted command on an allowlisted Tailscale/SSH host.", map[string]any{"host": strProp(), "command": strProp(), "timeout_seconds": intProp()}),
		toolDef("http_request", "Call an allowlisted HTTP endpoint.", map[string]any{"method": strProp(), "url": strProp(), "body": strProp()}),
		toolDef("send_qq_message", "Send a QQ private or group message.", map[string]any{"target_type": strProp(), "target_id": intProp(), "message": strProp()}),
		toolDef("bot_plugins", "List bot-platform external plugins and their running status.", map[string]any{}),
		toolDef("plugin_control", "Install, start, stop, restart, or uninstall a bot-platform external plugin through the admin API.", map[string]any{"action": strProp(), "name": strProp(), "repo_url": strProp(), "auto_start": boolProp()}),
		toolDef("plugin_config", "Safely list, read, preview, or set allowlisted plugin configuration fields. Protected fields cannot be changed. Guarded fields require confirm=true.", map[string]any{"action": strProp(), "plugin": strProp(), "key": strProp(), "value": map[string]any{}, "dry_run": boolProp(), "confirm": boolProp()}),
		toolDef("runtime_status", "Inspect this agent's runtime configuration, projects, skills, and recent tool audit.", map[string]any{}),
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

func strProp() map[string]any  { return map[string]any{"type": "string"} }
func intProp() map[string]any  { return map[string]any{"type": "integer"} }
func boolProp() map[string]any { return map[string]any{"type": "boolean"} }

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
	case "write_file":
		out = r.writeFile(raw)
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
	case "bot_plugins":
		out = r.botPlugins(ctx)
	case "plugin_control":
		out = r.pluginControl(ctx, raw)
	case "plugin_config":
		out = r.pluginConfig(ctx, raw)
	case "runtime_status":
		out = r.runtimeStatus()
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

func (r *ToolRegistry) isProtectedConfigWrite(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	policyDir, _ := filepath.Abs(r.cfg.ConfigPolicy.PolicyDir)
	backupDir, _ := filepath.Abs(r.cfg.ConfigPolicy.BackupDir)
	if strings.HasPrefix(abs, policyDir+string(os.PathSeparator)) || strings.HasPrefix(abs, backupDir+string(os.PathSeparator)) {
		return false
	}
	for _, allowed := range r.cfg.Permission.AllowedPaths {
		base, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		pluginsConfig := filepath.Join(base, "plugins-config")
		if filepath.Base(base) == "plugins-config" {
			pluginsConfig = base
		}
		if (abs == pluginsConfig || strings.HasPrefix(abs, pluginsConfig+string(os.PathSeparator))) && strings.HasSuffix(abs, ".json") {
			return true
		}
	}
	return false
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

func (r *ToolRegistry) writeFile(raw json.RawMessage) ToolResult {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(raw, &args)
	path, err := r.allowedPath(args.Path)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	if r.isProtectedConfigWrite(path) {
		return ToolResult{Error: "direct writes to plugin configuration JSON are blocked; use plugin_config"}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return ToolResult{Error: err.Error()}
	}
	if err := os.WriteFile(path, []byte(args.Content), 0644); err != nil {
		return ToolResult{Error: err.Error()}
	}
	return ToolResult{Content: "wrote " + path}
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

func (r *ToolRegistry) botPlugins(ctx context.Context) ToolResult {
	base := strings.TrimRight(r.cfg.Runtime.BotPlatformAdminURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/plugins", nil)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ToolResult{Error: fmt.Sprintf("admin API returned %s: %s", resp.Status, shortText(string(data), 800))}
	}
	return ToolResult{Content: string(data)}
}

func (r *ToolRegistry) pluginControl(ctx context.Context, raw json.RawMessage) ToolResult {
	var args struct {
		Action    string `json:"action"`
		Name      string `json:"name"`
		RepoURL   string `json:"repo_url"`
		AutoStart bool   `json:"auto_start"`
	}
	_ = json.Unmarshal(raw, &args)
	args.Action = strings.ToLower(strings.TrimSpace(args.Action))
	base := strings.TrimRight(r.cfg.Runtime.BotPlatformAdminURL, "/")
	var endpoint string
	body := map[string]any{}
	switch args.Action {
	case "install":
		if args.RepoURL == "" {
			return ToolResult{Error: "repo_url is required for install"}
		}
		endpoint = "/api/plugins/install"
		body["repo_url"] = args.RepoURL
		body["auto_start"] = args.AutoStart
	case "start", "stop", "uninstall":
		if args.Name == "" {
			return ToolResult{Error: "name is required for " + args.Action}
		}
		endpoint = "/api/plugins/" + args.Action
		body["name"] = args.Name
	case "restart":
		if args.Name == "" {
			return ToolResult{Error: "name is required for restart"}
		}
		stop := r.callAdmin(ctx, base+"/api/plugins/stop", map[string]any{"name": args.Name})
		if stop.Error != "" && !strings.Contains(stop.Error, "not running") {
			return stop
		}
		start := r.callAdmin(ctx, base+"/api/plugins/start", map[string]any{"name": args.Name})
		if start.Error != "" {
			return start
		}
		return ToolResult{Content: "restart requested\nstop: " + stop.Content + "\nstart: " + start.Content}
	default:
		return ToolResult{Error: "action must be install, start, stop, restart, or uninstall"}
	}
	return r.callAdmin(ctx, base+endpoint, body)
}

func (r *ToolRegistry) pluginConfig(ctx context.Context, raw json.RawMessage) ToolResult {
	var args struct {
		Action  string `json:"action"`
		Plugin  string `json:"plugin"`
		Key     string `json:"key"`
		Value   any    `json:"value"`
		DryRun  bool   `json:"dry_run"`
		Confirm bool   `json:"confirm"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	_ = decoder.Decode(&args)
	args.Action = strings.ToLower(strings.TrimSpace(args.Action))
	if args.Action == "" {
		args.Action = "list"
	}
	if args.Action == "list" {
		plugins, err := listConfigPolicies(r.cfg.ConfigPolicy.PolicyDir)
		if err != nil {
			return ToolResult{Error: err.Error()}
		}
		return ToolResult{Content: strings.Join(plugins, "\n")}
	}
	policy, err := loadConfigPolicy(r.cfg.ConfigPolicy.PolicyDir, args.Plugin)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	switch args.Action {
	case "schema":
		data, _ := json.MarshalIndent(policy, "", "  ")
		return ToolResult{Content: string(data)}
	case "get":
		view, err := readPluginConfig(policy)
		if err != nil {
			return ToolResult{Error: err.Error()}
		}
		data, _ := json.MarshalIndent(view, "", "  ")
		return ToolResult{Content: string(data)}
	case "diff", "set":
		if args.Key == "" {
			return ToolResult{Error: "key is required"}
		}
		if field, ok := policy.Allowed[args.Key]; ok && field.Guarded && args.Action == "set" && !args.Confirm {
			return ToolResult{Error: args.Key + " is guarded; preview with action=diff first and set confirm=true to apply"}
		}
		dryRun := args.Action == "diff" || args.DryRun
		change, err := applyPluginConfigChange(policy, r.cfg.ConfigPolicy.BackupDir, args.Key, args.Value, dryRun)
		if err != nil {
			return ToolResult{Error: err.Error()}
		}
		data, _ := json.MarshalIndent(change, "", "  ")
		if dryRun || !change.Restart {
			return ToolResult{Content: string(data)}
		}
		restart := r.pluginControl(ctx, mustJSON(map[string]any{"action": "restart", "name": policy.Plugin}))
		if restart.Error != "" {
			return ToolResult{Content: string(data), Error: "config updated but restart failed: " + restart.Error}
		}
		return ToolResult{Content: string(data) + "\nrestart requested"}
	default:
		return ToolResult{Error: "action must be list, schema, get, diff, or set"}
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func (r *ToolRegistry) callAdmin(ctx context.Context, endpoint string, body map[string]any) ToolResult {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ToolResult{Error: fmt.Sprintf("admin API returned %s: %s", resp.Status, shortText(string(data), 800))}
	}
	return ToolResult{Content: string(data)}
}

func (r *ToolRegistry) runtimeStatus() ToolResult {
	_ = r.skills.Load()
	var b strings.Builder
	b.WriteString("Agent runtime\n")
	b.WriteString("admin_api: " + r.cfg.Runtime.BotPlatformAdminURL + "\n")
	b.WriteString("workspace: " + r.cfg.Runtime.WorkspacePath + "\n")
	b.WriteString("container: " + r.cfg.Runtime.ContainerName + "\n")
	b.WriteString(fmt.Sprintf("model: %s\n", r.cfg.Model.Model))
	b.WriteString(fmt.Sprintf("auto_execute: %v\n", r.cfg.Permission.AutoExecute))
	b.WriteString("allowed_paths:\n")
	for _, p := range r.cfg.Permission.AllowedPaths {
		b.WriteString("- " + p + "\n")
	}
	b.WriteString("projects:\n")
	for _, p := range r.cfg.Projects {
		b.WriteString("- " + p.Name + ": " + p.Path)
		if p.RemoteHost != "" {
			b.WriteString(" on " + p.RemoteHost)
		}
		b.WriteString("\n")
	}
	b.WriteString("skills:\n")
	for _, s := range r.skills.All() {
		b.WriteString("- " + s.Name + ": " + s.Description + "\n")
	}
	r.state.mu.Lock()
	audit := append([]AuditRecord(nil), r.state.AuditLog...)
	r.state.mu.Unlock()
	if len(audit) > 5 {
		audit = audit[len(audit)-5:]
	}
	b.WriteString("recent_tool_audit:\n")
	for _, item := range audit {
		b.WriteString(fmt.Sprintf("- %s %s %s\n", item.Time.Format(time.RFC3339), item.Tool, item.Status))
	}
	return ToolResult{Content: b.String()}
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
	candidates := []string{p}
	if !filepath.IsAbs(p) && len(r.cfg.Permission.AllowedPaths) > 0 {
		candidates = append(candidates, filepath.Join(r.cfg.Permission.AllowedPaths[0], p))
	}
	for _, base := range r.cfg.Permission.AllowedPaths {
		baseAbs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		for _, candidate := range candidates {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(baseAbs, abs)
			if err == nil && (rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")) {
				return abs, nil
			}
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
