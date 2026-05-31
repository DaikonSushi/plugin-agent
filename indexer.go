package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Indexer struct {
	cfg   *Config
	state *State
	path  string
}

func NewIndexer(cfg *Config, state *State) *Indexer {
	return &Indexer{cfg: cfg, state: state, path: filepath.Join(cfg.DataDir, "project_index.md")}
}

func (i *Indexer) Rebuild() error {
	var b strings.Builder
	b.WriteString("# Project Index\n\n")
	b.WriteString("Generated: " + time.Now().Format(time.RFC3339) + "\n\n")
	for _, p := range i.cfg.Projects {
		b.WriteString("## " + p.Name + "\n")
		b.WriteString("Path: " + p.Path + "\n")
		if p.RemoteHost != "" {
			b.WriteString("Remote: " + p.RemoteHost + "\n")
		}
		if p.Description != "" {
			b.WriteString("Description: " + p.Description + "\n")
		}
		b.WriteString("\n")
		if p.RemoteHost != "" {
			b.WriteString("Remote projects are accessed via configured tools; no local scan performed.\n\n")
			continue
		}
		root, err := filepath.Abs(p.Path)
		if err != nil {
			b.WriteString("Error: " + err.Error() + "\n\n")
			continue
		}
		b.WriteString(scanProject(root, 180))
		b.WriteString("\n")
	}
	if err := os.MkdirAll(filepath.Dir(i.path), 0755); err != nil {
		return err
	}
	return os.WriteFile(i.path, []byte(b.String()), 0644)
}

func (i *Indexer) Read() (string, error) {
	if _, err := os.Stat(i.path); os.IsNotExist(err) {
		if err := i.Rebuild(); err != nil {
			return "", err
		}
	}
	data, err := os.ReadFile(i.path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func scanProject(root string, max int) string {
	var b strings.Builder
	readFirst := func(name string) {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err == nil {
			b.WriteString("### " + name + "\n")
			b.WriteString(shortText(string(data), 2000) + "\n\n")
		}
	}
	readFirst("README.md")
	readFirst("go.mod")
	readFirst("package.json")

	count := 0
	b.WriteString("### Files\n")
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || count >= max {
			return nil
		}
		name := d.Name()
		if d.IsDir() && shouldSkipDir(name) && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		b.WriteString(fmt.Sprintf("- %s\n", rel))
		count++
		return nil
	})
	return b.String()
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", "target", ".cache":
		return true
	default:
		return false
	}
}
