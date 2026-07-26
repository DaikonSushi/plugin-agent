package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type PluginConfigPolicy struct {
	Plugin        string                       `json:"plugin"`
	ConfigPath    string                       `json:"config_path"`
	RestartPlugin bool                         `json:"restart_plugin"`
	Allowed       map[string]ConfigFieldPolicy `json:"allowed"`
	Protected     []string                     `json:"protected"`
}

type ConfigFieldPolicy struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
	Guarded     bool     `json:"guarded,omitempty"`
}

type ConfigChange struct {
	Plugin  string `json:"plugin"`
	Key     string `json:"key"`
	Old     any    `json:"old"`
	New     any    `json:"new"`
	Backup  string `json:"backup,omitempty"`
	DryRun  bool   `json:"dry_run"`
	Restart bool   `json:"restart"`
}

func loadConfigPolicy(dir, plugin string) (*PluginConfigPolicy, error) {
	plugin = strings.TrimSpace(plugin)
	if plugin == "" || strings.Contains(plugin, "/") || strings.Contains(plugin, "..") {
		return nil, fmt.Errorf("invalid plugin name")
	}
	path := filepath.Join(dir, plugin+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var policy PluginConfigPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, err
	}
	if policy.Plugin == "" {
		policy.Plugin = plugin
	}
	if policy.ConfigPath == "" {
		return nil, fmt.Errorf("config_path is required")
	}
	if policy.Allowed == nil {
		policy.Allowed = map[string]ConfigFieldPolicy{}
	}
	return &policy, nil
}

func listConfigPolicies(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	plugins := make([]string, 0, len(entries))
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		plugins = append(plugins, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(plugins)
	return plugins, nil
}

func applyPluginConfigChange(policy *PluginConfigPolicy, backupDir, key string, value any, dryRun bool) (*ConfigChange, error) {
	field, ok := policy.Allowed[key]
	if !ok {
		if stringInSlice(key, policy.Protected) {
			return nil, fmt.Errorf("%s is protected and cannot be changed through chat", key)
		}
		return nil, fmt.Errorf("%s is not allowlisted for chat configuration", key)
	}
	newValue, err := normalizeConfigValue(value, field)
	if err != nil {
		return nil, err
	}
	config, err := readJSONMap(policy.ConfigPath)
	if err != nil {
		return nil, err
	}
	oldValue := config[key]
	change := &ConfigChange{Plugin: policy.Plugin, Key: key, Old: oldValue, New: newValue, DryRun: dryRun, Restart: policy.RestartPlugin}
	if dryRun {
		return change, nil
	}
	backup, err := backupConfig(policy.ConfigPath, backupDir, policy.Plugin)
	if err != nil {
		return nil, err
	}
	config[key] = newValue
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(policy.ConfigPath, append(data, '\n'), 0644); err != nil {
		return nil, err
	}
	change.Backup = backup
	return change, nil
}

func readPluginConfig(policy *PluginConfigPolicy) (map[string]any, error) {
	config, err := readJSONMap(policy.ConfigPath)
	if err != nil {
		return nil, err
	}
	view := map[string]any{}
	for key := range policy.Allowed {
		view[key] = config[key]
	}
	for _, key := range policy.Protected {
		if _, ok := config[key]; ok {
			view[key] = "<protected>"
		}
	}
	return view, nil
}

func readJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return config, nil
}

func backupConfig(path, backupDir, plugin string) (string, error) {
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	backup := filepath.Join(backupDir, fmt.Sprintf("%s_%s.json", plugin, time.Now().Format("20060102_150405")))
	return backup, os.WriteFile(backup, data, 0644)
}

func normalizeConfigValue(value any, field ConfigFieldPolicy) (any, error) {
	switch field.Type {
	case "bool":
		v, ok := value.(bool)
		if !ok {
			if s, ok := value.(string); ok {
				s = strings.ToLower(strings.TrimSpace(s))
				if s == "true" || s == "on" || s == "1" || s == "yes" {
					return true, nil
				}
				if s == "false" || s == "off" || s == "0" || s == "no" {
					return false, nil
				}
			}
			return nil, fmt.Errorf("value must be bool")
		}
		return v, nil
	case "int":
		v, err := numberAsFloat(value)
		if err != nil {
			return nil, fmt.Errorf("value must be int")
		}
		if v != float64(int64(v)) {
			return nil, fmt.Errorf("value must be int")
		}
		if field.Min != nil && v < *field.Min {
			return nil, fmt.Errorf("value must be >= %g", *field.Min)
		}
		if field.Max != nil && v > *field.Max {
			return nil, fmt.Errorf("value must be <= %g", *field.Max)
		}
		return int64(v), nil
	case "number":
		v, err := numberAsFloat(value)
		if err != nil {
			return nil, fmt.Errorf("value must be number")
		}
		if field.Min != nil && v < *field.Min {
			return nil, fmt.Errorf("value must be >= %g", *field.Min)
		}
		if field.Max != nil && v > *field.Max {
			return nil, fmt.Errorf("value must be <= %g", *field.Max)
		}
		return v, nil
	case "string":
		v, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("value must be string")
		}
		if len(field.Enum) > 0 && !stringInSlice(v, field.Enum) {
			return nil, fmt.Errorf("value must be one of: %s", strings.Join(field.Enum, ", "))
		}
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported field type %q", field.Type)
	}
}

func numberAsFloat(value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case json.Number:
		return v.Float64()
	default:
		return 0, fmt.Errorf("not a number")
	}
}

func stringInSlice(s string, items []string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}
