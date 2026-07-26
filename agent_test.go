package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNextRunDurationAndDaily(t *testing.T) {
	loc := time.FixedZone("test", 8*3600)
	now := time.Date(2026, 5, 31, 10, 0, 0, 0, loc)
	got, err := NextRun("10m", now, loc)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("duration got %s", got)
	}
	got, err = NextRun("daily@09:30", now, loc)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 6, 1, 9, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("daily got %s want %s", got, want)
	}
}

func TestOneShotScheduleDetection(t *testing.T) {
	for _, spec := range []string{"once:2026-06-01 10:30", "2026-06-01 10:30", "10m"} {
		if !isOneShotSchedule(spec) {
			t.Fatalf("%s should be one-shot", spec)
		}
	}
	for _, spec := range []string{"daily@09:30", "weekly@Mon,10:30"} {
		if isOneShotSchedule(spec) {
			t.Fatalf("%s should be recurring", spec)
		}
	}
}

func TestAllowedPathRejectsOutside(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Permission.AllowedPaths = []string{dir}
	state := &State{Listeners: map[string]bool{}, Conversations: map[string][]ChatRecord{}}
	tr := NewToolRegistry(cfg, state, nil)
	if _, err := tr.allowedPath(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("inside path rejected: %v", err)
	}
	if _, err := tr.allowedPath(filepath.Dir(dir)); err == nil {
		t.Fatal("outside path accepted")
	}
}

func TestAllowedPathRelativeUsesFirstAllowedPath(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Permission.AllowedPaths = []string{dir}
	state := &State{Listeners: map[string]bool{}, Conversations: map[string][]ChatRecord{}}
	tr := NewToolRegistry(cfg, state, nil)
	got, err := tr.allowedPath("nested/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "nested", "a.txt")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestAllowedCommand(t *testing.T) {
	cfg := DefaultConfig()
	state := &State{Listeners: map[string]bool{}, Conversations: map[string][]ChatRecord{}}
	tr := NewToolRegistry(cfg, state, nil)
	if err := tr.allowedCommand("rg foo ."); err != nil {
		t.Fatalf("allowlisted command rejected: %v", err)
	}
	if err := tr.allowedCommand("rm -rf /"); err == nil {
		t.Fatal("denylisted command accepted")
	}
	if err := tr.allowedCommand("python script.py"); err == nil {
		t.Fatal("non-allowlisted command accepted")
	}
}

func TestParseSkill(t *testing.T) {
	skill := parseSkill("fallback", "name: ops\ndescription: ops tasks\ntriggers: deploy, logs\n\nBody")
	if skill.Name != "ops" || skill.Description != "ops tasks" || len(skill.Triggers) != 2 {
		t.Fatalf("unexpected skill: %+v", skill)
	}
}

func TestRoleWhitelist(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AdminUsers = []int64{1}
	cfg.Access.GroupWhitelist = []int64{100}
	cfg.Access.RoleWhitelist = []string{"owner", "admin"}
	if !cfg.IsAllowedMessage("group", 100, 2, "admin") {
		t.Fatal("admin role should be allowed")
	}
	if cfg.IsAllowedMessage("group", 100, 2, "member") {
		t.Fatal("member role should be rejected")
	}
	if cfg.IsAllowedMessage("group", 200, 2, "admin") {
		t.Fatal("non-whitelisted group should be rejected")
	}
	if !cfg.IsAllowedMessage("group", 200, 1, "member") {
		t.Fatal("configured admin should bypass access whitelist")
	}
}

func TestToolCallDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Permission.AutoExecute = false
	cfg.DataDir = t.TempDir()
	state, err := LoadState(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	tr := NewToolRegistry(cfg, state, nil)
	raw, _ := json.Marshal(map[string]any{"path": "."})
	result := tr.Execute(nil, "list_files", raw)
	if !strings.Contains(result.Error, "disabled") {
		t.Fatalf("expected disabled error, got %+v", result)
	}
}

func TestWriteFileTool(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Permission.AllowedPaths = []string{dir}
	cfg.DataDir = t.TempDir()
	state, err := LoadState(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	tr := NewToolRegistry(cfg, state, nil)
	raw, _ := json.Marshal(map[string]any{"path": filepath.Join(dir, "nested", "a.txt"), "content": "hello"})
	result := tr.Execute(nil, "write_file", raw)
	if result.Error != "" {
		t.Fatalf("write_file failed: %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(dir, "nested", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected content: %s", data)
	}
}

func TestWriteFileBlocksPluginConfigJSON(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Permission.AllowedPaths = []string{filepath.Join(dir, "plugins-config")}
	cfg.ConfigPolicy.PolicyDir = filepath.Join(dir, "plugins-config", "config-policies")
	cfg.ConfigPolicy.BackupDir = filepath.Join(dir, "plugins-config", "config-backups")
	state := &State{Listeners: map[string]bool{}, Conversations: map[string][]ChatRecord{}}
	tr := NewToolRegistry(cfg, state, nil)

	raw, _ := json.Marshal(map[string]any{"path": filepath.Join(dir, "plugins-config", "showmejm", "config.json"), "content": "{}"})
	result := tr.Execute(nil, "write_file", raw)
	if !strings.Contains(result.Error, "plugin_config") {
		t.Fatalf("expected direct config write to be blocked, got %+v", result)
	}
}

func TestPluginConfigSetAllowsOnlyPolicyFields(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "policies")
	backupDir := filepath.Join(dir, "backups")
	configPath := filepath.Join(dir, "showmejm", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(policyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"auto_find_jm":true,"admin_users":[1]}`), 0644); err != nil {
		t.Fatal(err)
	}
	policy := PluginConfigPolicy{
		Plugin:     "showmejm",
		ConfigPath: configPath,
		Allowed: map[string]ConfigFieldPolicy{
			"auto_find_jm": {Type: "bool"},
		},
		Protected: []string{"admin_users"},
	}
	data, _ := json.Marshal(policy)
	if err := os.WriteFile(filepath.Join(policyDir, "showmejm.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.ConfigPolicy.PolicyDir = policyDir
	cfg.ConfigPolicy.BackupDir = backupDir
	state := &State{Listeners: map[string]bool{}, Conversations: map[string][]ChatRecord{}}
	tr := NewToolRegistry(cfg, state, nil)

	raw, _ := json.Marshal(map[string]any{"action": "set", "plugin": "showmejm", "key": "auto_find_jm", "value": false})
	result := tr.Execute(context.Background(), "plugin_config", raw)
	if result.Error != "" {
		t.Fatalf("plugin_config set failed: %+v", result)
	}
	updated, err := readJSONMap(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated["auto_find_jm"] != false {
		t.Fatalf("auto_find_jm was not updated: %+v", updated)
	}

	raw, _ = json.Marshal(map[string]any{"action": "set", "plugin": "showmejm", "key": "admin_users", "value": []int{2}})
	result = tr.Execute(context.Background(), "plugin_config", raw)
	if !strings.Contains(result.Error, "protected") {
		t.Fatalf("expected protected field rejection, got %+v", result)
	}
}

func TestPluginConfigGuardedFieldRequiresConfirm(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "policies")
	configPath := filepath.Join(dir, "showmejm", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(policyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"max_concurrent_tasks":2}`), 0644); err != nil {
		t.Fatal(err)
	}
	policy := PluginConfigPolicy{
		Plugin:     "showmejm",
		ConfigPath: configPath,
		Allowed: map[string]ConfigFieldPolicy{
			"max_concurrent_tasks": {Type: "int", Guarded: true},
		},
	}
	data, _ := json.Marshal(policy)
	if err := os.WriteFile(filepath.Join(policyDir, "showmejm.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.ConfigPolicy.PolicyDir = policyDir
	cfg.ConfigPolicy.BackupDir = filepath.Join(dir, "backups")
	state := &State{Listeners: map[string]bool{}, Conversations: map[string][]ChatRecord{}}
	tr := NewToolRegistry(cfg, state, nil)

	raw, _ := json.Marshal(map[string]any{"action": "set", "plugin": "showmejm", "key": "max_concurrent_tasks", "value": 3})
	result := tr.Execute(context.Background(), "plugin_config", raw)
	if !strings.Contains(result.Error, "guarded") {
		t.Fatalf("expected guarded rejection, got %+v", result)
	}
	raw, _ = json.Marshal(map[string]any{"action": "set", "plugin": "showmejm", "key": "max_concurrent_tasks", "value": 3, "confirm": true})
	result = tr.Execute(context.Background(), "plugin_config", raw)
	if result.Error != "" {
		t.Fatalf("confirmed guarded set failed: %+v", result)
	}
}

func TestEnsureDefaultSkills(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureDefaultSkills(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "qq_bot_platform", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "QQ Bot Platform Development Skill") {
		t.Fatalf("unexpected default skill: %s", data)
	}
}

func TestPlainTextMarkdown(t *testing.T) {
	got := plainTextMarkdown("## 标题\n**加粗** 和 `code`\n[链接](https://example.com)\n```go\nfmt.Println(1)\n```")
	for _, bad := range []string{"##", "**", "`", "```"} {
		if strings.Contains(got, bad) {
			t.Fatalf("markdown marker %q not removed from %q", bad, got)
		}
	}
	if !strings.Contains(got, "链接 (https://example.com)") {
		t.Fatalf("link not converted: %q", got)
	}
}

func TestAgentInfoIsFallback(t *testing.T) {
	info := (&AgentPlugin{}).Info()
	if !info.HandleAllMessages || !info.Fallback || info.MessagePriority >= 0 {
		t.Fatalf("agent should be low-priority fallback, got %+v", info)
	}
}

func TestUseHermesConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UseHermes("", "")
	if cfg.Model.Provider != defaultHermesProvider {
		t.Fatalf("provider = %q", cfg.Model.Provider)
	}
	if cfg.Model.BaseURL != defaultHermesBaseURL {
		t.Fatalf("base url = %q", cfg.Model.BaseURL)
	}
	if cfg.Model.APIKeyEnv != defaultHermesAPIKeyEnv {
		t.Fatalf("api key env = %q", cfg.Model.APIKeyEnv)
	}
	if cfg.Model.Model != defaultHermesModel {
		t.Fatalf("model = %q", cfg.Model.Model)
	}
	if !cfg.Model.DisableLocalTools || !cfg.Model.HermesSession {
		t.Fatalf("hermes flags not enabled: %+v", cfg.Model)
	}
}

func TestHermesChatRequestUsesSessionAndNoLocalTools(t *testing.T) {
	t.Setenv("TEST_HERMES_KEY", "secret")
	var sawSession bool
	var sawTools bool
	var sawModel bool
	handler := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected authorization: %s", r.Header.Get("Authorization"))
		}
		sawSession = r.Header.Get("X-Hermes-Session-Key") == "qq-bot:group:123"
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, sawTools = body["tools"]
		sawModel = body["model"] == defaultHermesModel
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)),
		}, nil
	})

	cfg := DefaultConfig()
	cfg.UseHermes("http://hermes.local/v1", "TEST_HERMES_KEY")
	state := &State{Listeners: map[string]bool{}, Conversations: map[string][]ChatRecord{}}
	engine := NewAgentEngine(cfg, state, NewToolRegistry(cfg, state, nil))
	engine.client.Transport = handler
	msg, err := engine.chat(context.Background(), "group:123", []ChatMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "ok" {
		t.Fatalf("unexpected response: %+v", msg)
	}
	if !sawSession {
		t.Fatal("Hermes session header was not set")
	}
	if sawTools {
		t.Fatal("Hermes request should not include local tool definitions")
	}
	if !sawModel {
		t.Fatal("Hermes request did not use hermes-agent model")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestRuntimeStatusTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.SkillsDir = filepath.Join(t.TempDir(), "skills")
	if err := EnsureDefaultSkills(cfg.SkillsDir); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	tr := NewToolRegistry(cfg, state, nil)
	result := tr.Execute(context.Background(), "runtime_status", nil)
	if result.Error != "" {
		t.Fatalf("runtime_status failed: %+v", result)
	}
	if !strings.Contains(result.Content, "Agent runtime") || !strings.Contains(result.Content, "qq_bot") {
		t.Fatalf("unexpected runtime status: %s", result.Content)
	}
}

func TestIndexerCreatesFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Demo"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.Projects = []ProjectConfig{{Name: "demo", Path: root}}
	idx := NewIndexer(cfg, nil)
	if err := idx.Rebuild(); err != nil {
		t.Fatal(err)
	}
	text, err := idx.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "# Demo") {
		t.Fatalf("index missing README: %s", text)
	}
}
