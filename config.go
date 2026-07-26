package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	configPath             = "plugins-config/agent/config.json"
	defaultOpenAIProvider  = "openai-compatible"
	defaultHermesProvider  = "hermes"
	defaultHermesBaseURL   = "http://hermes:8642/v1"
	defaultHermesAPIKeyEnv = "HERMES_API_SERVER_KEY"
	defaultHermesModel     = "hermes-agent"
)

type Config struct {
	Model        ModelConfig               `json:"model"`
	Permission   PermissionConfig          `json:"permissions"`
	Projects     []ProjectConfig           `json:"projects"`
	SkillsDir    string                    `json:"skills_dir"`
	DataDir      string                    `json:"data_dir"`
	Schedule     ScheduleConfig            `json:"schedule"`
	Access       AccessConfig              `json:"access"`
	Runtime      RuntimeConfig             `json:"runtime"`
	Response     ResponseConfig            `json:"response"`
	ChatTrigger  ChatTriggerConfig         `json:"chat_triggers"`
	ConfigPolicy ConfigPolicyRuntimeConfig `json:"config_policy"`
	AdminUsers   []int64                   `json:"admin_users"`
}

type ModelConfig struct {
	Provider          string   `json:"provider,omitempty"`
	BaseURL           string   `json:"base_url"`
	APIKeyEnv         string   `json:"api_key_env"`
	APIKeyFile        string   `json:"api_key_file,omitempty"`
	Model             string   `json:"model"`
	ModelOptions      []string `json:"model_options,omitempty"`
	Temperature       float64  `json:"temperature"`
	TimeoutSec        int      `json:"timeout_seconds"`
	MaxSteps          int      `json:"max_steps"`
	DisableLocalTools bool     `json:"disable_local_tools,omitempty"`
	HermesSession     bool     `json:"hermes_session,omitempty"`
}

type PermissionConfig struct {
	AutoExecute        bool     `json:"auto_execute"`
	AllowedPaths       []string `json:"allowed_paths"`
	AllowedHosts       []string `json:"allowed_hosts"`
	AllowedHTTPHosts   []string `json:"allowed_http_hosts"`
	AllowedCommandPref []string `json:"allowed_command_prefixes"`
	DeniedCommandParts []string `json:"denied_command_parts"`
}

type ProjectConfig struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	RemoteHost  string `json:"remote_host,omitempty"`
	Description string `json:"description,omitempty"`
}

type ScheduleConfig struct {
	Enabled      bool   `json:"enabled"`
	Timezone     string `json:"timezone"`
	CheckSeconds int    `json:"check_seconds"`
}

type AccessConfig struct {
	PersonWhitelist []int64  `json:"person_whitelist"`
	GroupWhitelist  []int64  `json:"group_whitelist"`
	RoleWhitelist   []string `json:"role_whitelist"`
}

type RuntimeConfig struct {
	BotPlatformAdminURL string `json:"bot_platform_admin_url"`
	WorkspacePath       string `json:"workspace_path"`
	ContainerName       string `json:"container_name"`
}

type ResponseConfig struct {
	SendWorkingMessage bool `json:"send_working_message"`
	PlainText          bool `json:"plain_text"`
}

type ChatTriggerConfig struct {
	RequireMentionInGroup bool `json:"require_mention_in_group"`
	HandleAllPrivate      bool `json:"handle_all_private"`
}

type ConfigPolicyRuntimeConfig struct {
	PolicyDir string `json:"policy_dir"`
	BackupDir string `json:"backup_dir"`
}

func DefaultConfig() *Config {
	return &Config{
		Model: ModelConfig{
			Provider:     defaultOpenAIProvider,
			BaseURL:      "https://api.openai.com/v1",
			APIKeyEnv:    "OPENAI_API_KEY",
			Model:        "gpt-4.1-mini",
			ModelOptions: []string{"gpt-4.1-mini"},
			Temperature:  0.2,
			TimeoutSec:   120,
			MaxSteps:     8,
		},
		Permission: PermissionConfig{
			AutoExecute:      true,
			AllowedPaths:     []string{"."},
			AllowedHosts:     []string{},
			AllowedHTTPHosts: []string{},
			AllowedCommandPref: []string{
				"pwd", "ls", "cat ", "sed ", "rg ", "find ", "git ", "go test", "go build", "go mod ", "gofmt ",
			},
			DeniedCommandParts: []string{
				"rm -rf /", "mkfs", "dd if=", ":(){", "shutdown", "reboot",
			},
		},
		Projects: []ProjectConfig{
			{Name: "qq_bot", Path: ".", Description: "Current QQ bot workspace"},
		},
		SkillsDir: "plugins-config/agent/skills",
		DataDir:   "plugins-config/agent/data",
		Schedule: ScheduleConfig{
			Enabled:      true,
			Timezone:     "Asia/Shanghai",
			CheckSeconds: 5,
		},
		Access: AccessConfig{
			PersonWhitelist: []int64{},
			GroupWhitelist:  []int64{},
			RoleWhitelist:   []string{},
		},
		Runtime: RuntimeConfig{
			BotPlatformAdminURL: "http://127.0.0.1:8080",
			WorkspacePath:       "/workspace/qq_bot",
			ContainerName:       "qq-bot-all-in-one",
		},
		Response: ResponseConfig{
			SendWorkingMessage: false,
			PlainText:          true,
		},
		ChatTrigger: ChatTriggerConfig{
			RequireMentionInGroup: true,
			HandleAllPrivate:      true,
		},
		ConfigPolicy: ConfigPolicyRuntimeConfig{
			PolicyDir: "plugins-config/config-policies",
			BackupDir: "plugins-config/config-backups",
		},
		AdminUsers: []int64{},
	}
}

func LoadConfig() (*Config, error) {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return nil, err
	}
	cfg := DefaultConfig()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := cfg.Save(configPath); err != nil {
			return nil, err
		}
		return cfg, cfg.ensureDirs()
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	return cfg, cfg.ensureDirs()
}

func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (c *Config) applyDefaults() {
	if c.Model.Provider == "" {
		c.Model.Provider = defaultOpenAIProvider
	}
	if c.Model.BaseURL == "" {
		c.Model.BaseURL = "https://api.openai.com/v1"
	}
	if c.Model.APIKeyEnv == "" {
		c.Model.APIKeyEnv = "OPENAI_API_KEY"
	}
	if c.Model.Model == "" {
		c.Model.Model = "gpt-4.1-mini"
	}
	if len(c.Model.ModelOptions) == 0 {
		c.Model.ModelOptions = []string{c.Model.Model}
	}
	if c.Model.TimeoutSec <= 0 {
		c.Model.TimeoutSec = 120
	}
	if c.Model.MaxSteps <= 0 {
		c.Model.MaxSteps = 8
	}
	if c.SkillsDir == "" {
		c.SkillsDir = "plugins-config/agent/skills"
	}
	if c.DataDir == "" {
		c.DataDir = "plugins-config/agent/data"
	}
	if c.Schedule.Timezone == "" {
		c.Schedule.Timezone = "Asia/Shanghai"
	}
	if !hasJSONField(configPath, "schedule", "enabled") {
		c.Schedule.Enabled = true
	}
	if c.Schedule.CheckSeconds <= 0 {
		c.Schedule.CheckSeconds = 5
	}
	if len(c.Permission.AllowedPaths) == 0 {
		c.Permission.AllowedPaths = []string{"."}
	}
	if len(c.Permission.DeniedCommandParts) == 0 {
		c.Permission.DeniedCommandParts = DefaultConfig().Permission.DeniedCommandParts
	}
	if c.Runtime.BotPlatformAdminURL == "" {
		c.Runtime.BotPlatformAdminURL = "http://127.0.0.1:8080"
	}
	if c.Runtime.WorkspacePath == "" {
		c.Runtime.WorkspacePath = "/workspace/qq_bot"
	}
	if c.Runtime.ContainerName == "" {
		c.Runtime.ContainerName = "qq-bot-all-in-one"
	}
	if !c.Response.PlainText {
		c.Response.PlainText = true
	}
	if c.ConfigPolicy.PolicyDir == "" {
		c.ConfigPolicy.PolicyDir = "plugins-config/config-policies"
	}
	if c.ConfigPolicy.BackupDir == "" {
		c.ConfigPolicy.BackupDir = "plugins-config/config-backups"
	}
}

func hasJSONField(path string, keys ...string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return false
	}
	current := value
	for _, key := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = obj[key]
		if !ok {
			return false
		}
	}
	return true
}

func (c *Config) UseHermes(baseURL, apiKeyEnv string) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultHermesBaseURL
	}
	if strings.TrimSpace(apiKeyEnv) == "" {
		apiKeyEnv = defaultHermesAPIKeyEnv
	}
	c.Model.Provider = defaultHermesProvider
	c.Model.BaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	c.Model.APIKeyEnv = strings.TrimSpace(apiKeyEnv)
	c.Model.APIKeyFile = ""
	c.Model.Model = defaultHermesModel
	c.Model.ModelOptions = []string{defaultHermesModel}
	c.Model.DisableLocalTools = true
	c.Model.HermesSession = true
}

func (c *Config) UseOpenAICompatible(baseURL, model, apiKeyEnv string) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if strings.TrimSpace(model) == "" {
		model = "gpt-4.1-mini"
	}
	if strings.TrimSpace(apiKeyEnv) == "" {
		apiKeyEnv = "OPENAI_API_KEY"
	}
	c.Model.Provider = defaultOpenAIProvider
	c.Model.BaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	c.Model.APIKeyEnv = strings.TrimSpace(apiKeyEnv)
	c.Model.APIKeyFile = ""
	c.Model.Model = strings.TrimSpace(model)
	c.Model.ModelOptions = []string{c.Model.Model}
	c.Model.DisableLocalTools = false
	c.Model.HermesSession = false
}

func (c *Config) ensureDirs() error {
	if err := os.MkdirAll(c.SkillsDir, 0755); err != nil {
		return err
	}
	if err := EnsureDefaultSkills(c.SkillsDir); err != nil {
		return err
	}
	if err := os.MkdirAll(c.DataDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(c.ConfigPolicy.PolicyDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(c.ConfigPolicy.BackupDir, 0755); err != nil {
		return err
	}
	return nil
}

func (c *Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Schedule.Timezone)
	if err != nil {
		return time.Local
	}
	return loc
}

func (c *Config) IsAdmin(userID int64) bool {
	if len(c.AdminUsers) == 0 {
		return true
	}
	for _, id := range c.AdminUsers {
		if id == userID {
			return true
		}
	}
	return false
}

func (c *Config) IsAllowedMessage(msgType string, groupID, userID int64, role string) bool {
	if c.IsAdmin(userID) {
		return true
	}
	if msgType == "group" {
		if !int64Allowed(groupID, c.Access.GroupWhitelist) {
			return false
		}
		if !roleAllowed(role, c.Access.RoleWhitelist) {
			return false
		}
	}
	return int64Allowed(userID, c.Access.PersonWhitelist)
}

func int64Allowed(id int64, whitelist []int64) bool {
	if len(whitelist) == 0 {
		return true
	}
	for _, item := range whitelist {
		if item == id {
			return true
		}
	}
	return false
}

func roleAllowed(role string, whitelist []string) bool {
	if len(whitelist) == 0 {
		return true
	}
	for _, item := range whitelist {
		if item == role {
			return true
		}
	}
	return false
}
