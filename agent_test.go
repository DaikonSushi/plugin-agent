package main

import (
	"encoding/json"
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
