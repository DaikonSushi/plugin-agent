package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DaikonSushi/bot-platform/pkg/pluginsdk"
)

type AgentPlugin struct {
	cfg       *Config
	state     *State
	tools     *ToolRegistry
	engine    *AgentEngine
	scheduler *Scheduler
}

func (p *AgentPlugin) Info() pluginsdk.PluginInfo {
	return pluginsdk.PluginInfo{
		Name:        "agent",
		Version:     "0.2.0",
		Description: "Personal AI agent with local project awareness, skills, tools, and scheduled tasks",
		Author:      "hovanzhang",
		Commands: []string{
			"ai", "agent",
		},
		HandleAllMessages: true,
	}
}

func (p *AgentPlugin) OnStart(bot *pluginsdk.BotClient) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	state, err := LoadState(cfg.DataDir)
	if err != nil {
		return err
	}
	p.cfg = cfg
	p.state = state
	p.tools = NewToolRegistry(cfg, state, bot)
	p.scheduler = NewScheduler(cfg, state, bot)
	p.tools.scheduler = p.scheduler
	p.engine = NewAgentEngine(cfg, state, p.tools)
	p.scheduler.SetEngine(p.engine)
	p.scheduler.Start()
	bot.Log("info", "Agent plugin started")
	return nil
}

func (p *AgentPlugin) OnStop() error {
	if p.scheduler != nil {
		p.scheduler.Stop()
	}
	return nil
}

func (p *AgentPlugin) OnMessage(ctx context.Context, bot *pluginsdk.BotClient, msg *pluginsdk.Message) bool {
	if p == nil || p.state == nil || p.engine == nil {
		return false
	}
	if !p.allowed(msg) {
		return false
	}
	key := targetKey(msg.Type, msg.GroupID, msg.UserID)
	if msg.Type == "private" && p.cfg.ChatTrigger.HandleAllPrivate {
		p.handlePrompt(ctx, bot, msg, msg.Text)
		return true
	}
	if p.state.Listeners[key] && shouldHandleGroupMessage(msg, p.cfg) {
		p.handlePrompt(ctx, bot, msg, msg.Text)
		return true
	}
	return false
}

func (p *AgentPlugin) OnCommand(ctx context.Context, bot *pluginsdk.BotClient, cmd string, args []string, msg *pluginsdk.Message) bool {
	if !p.allowed(msg) {
		bot.Reply(msg, pluginsdk.Text("抱歉，您没有使用 agent 的权限"))
		return true
	}
	switch cmd {
	case "ai":
		if len(args) == 0 {
			bot.Reply(msg, pluginsdk.Text("Usage: /ai <prompt>"))
			return true
		}
		p.handlePrompt(ctx, bot, msg, strings.Join(args, " "))
		return true
	case "agent":
		p.handleAgentCommand(ctx, bot, args, msg)
		return true
	default:
		return false
	}
}

func (p *AgentPlugin) allowed(msg *pluginsdk.Message) bool {
	role := ""
	if msg.Sender != nil {
		role = msg.Sender.Role
	}
	return p.cfg.IsAllowedMessage(msg.Type, msg.GroupID, msg.UserID, role)
}

func (p *AgentPlugin) handlePrompt(ctx context.Context, bot *pluginsdk.BotClient, msg *pluginsdk.Message, prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return
	}
	key := targetKey(msg.Type, msg.GroupID, msg.UserID)
	go func() {
		_, _ = bot.Reply(msg, pluginsdk.Text("Agent is working..."))
		cctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.cfg.Model.TimeoutSec)*time.Second)
		defer cancel()
		result, err := p.engine.Run(cctx, key, prompt)
		if err != nil {
			result = "Agent failed: " + err.Error()
		}
		_, _ = bot.Reply(msg, pluginsdk.Text(result))
	}()
}

func (p *AgentPlugin) handleAgentCommand(ctx context.Context, bot *pluginsdk.BotClient, args []string, msg *pluginsdk.Message) {
	if len(args) == 0 || args[0] == "help" {
		bot.Reply(msg, pluginsdk.Text(strings.TrimSpace(`
Agent commands:
/ai <prompt>
/agent on|off
/agent status
/agent tasks
/agent task add <schedule> <prompt>
/agent task del <id>
/agent task pause <id>
/agent task resume <id>
/agent skills
/agent index
/agent panic
Schedules: 10m, 2h, once:2026-06-01 09:30, daily@09:30, weekly@Mon,09:30
`)))
		return
	}
	switch args[0] {
	case "on", "off":
		key := targetKey(msg.Type, msg.GroupID, msg.UserID)
		p.state.mu.Lock()
		p.state.Listeners[key] = args[0] == "on"
		_ = p.state.saveLocked()
		p.state.mu.Unlock()
		bot.Reply(msg, pluginsdk.Text("Agent listener "+args[0]+" for "+key))
	case "status":
		p.handleStatus(bot, msg)
	case "tasks":
		p.handleTasks(bot, msg)
	case "task":
		p.handleTaskCommand(bot, args[1:], msg)
	case "skills":
		_ = p.tools.skills.Load()
		var lines []string
		for _, skill := range p.tools.skills.All() {
			lines = append(lines, fmt.Sprintf("- %s: %s", skill.Name, skill.Description))
		}
		if len(lines) == 0 {
			lines = append(lines, "No skills found in "+p.cfg.SkillsDir)
		}
		bot.Reply(msg, pluginsdk.Text(strings.Join(lines, "\n")))
	case "index":
		err := p.tools.indexer.Rebuild()
		if err != nil {
			bot.Reply(msg, pluginsdk.Text("Index failed: "+err.Error()))
			return
		}
		bot.Reply(msg, pluginsdk.Text("Project index rebuilt."))
	case "panic":
		p.cfg.Permission.AutoExecute = false
		_ = p.cfg.Save(configPath)
		bot.Reply(msg, pluginsdk.Text("Tool auto execution disabled in config. Restart plugin to enforce all runtime paths."))
	default:
		bot.Reply(msg, pluginsdk.Text("Unknown /agent command. Use /agent help"))
	}
}

func (p *AgentPlugin) handleStatus(bot *pluginsdk.BotClient, msg *pluginsdk.Message) {
	text := fmt.Sprintf(
		"Agent status\nModel: %s\nBase URL: %s\nAuto execute: %v\nProjects: %d\nTasks: %d\nSkills: %d",
		p.cfg.Model.Model,
		p.cfg.Model.BaseURL,
		p.cfg.Permission.AutoExecute,
		len(p.cfg.Projects),
		len(p.scheduler.List()),
		len(p.tools.skills.All()),
	)
	bot.Reply(msg, pluginsdk.Text(text))
}

func (p *AgentPlugin) handleTasks(bot *pluginsdk.BotClient, msg *pluginsdk.Message) {
	tasks := p.scheduler.List()
	if len(tasks) == 0 {
		bot.Reply(msg, pluginsdk.Text("No scheduled tasks."))
		return
	}
	var lines []string
	for _, task := range tasks {
		state := "paused"
		if task.Enabled {
			state = "enabled"
		}
		lines = append(lines, fmt.Sprintf("%s [%s] %s next=%s schedule=%s", task.ID, state, task.Name, task.NextRunAt.Format("2006-01-02 15:04"), task.Schedule))
	}
	bot.Reply(msg, pluginsdk.Text(strings.Join(lines, "\n")))
}

func (p *AgentPlugin) handleTaskCommand(bot *pluginsdk.BotClient, args []string, msg *pluginsdk.Message) {
	if len(args) == 0 {
		bot.Reply(msg, pluginsdk.Text("Usage: /agent task add|del|pause|resume ..."))
		return
	}
	switch args[0] {
	case "add":
		if len(args) < 3 {
			bot.Reply(msg, pluginsdk.Text("Usage: /agent task add <schedule> <prompt>"))
			return
		}
		targetType := "private"
		targetID := msg.UserID
		if msg.Type == "group" {
			targetType = "group"
			targetID = msg.GroupID
		}
		task, err := p.scheduler.Add("", strings.Join(args[2:], " "), targetType, targetID, args[1])
		if err != nil {
			bot.Reply(msg, pluginsdk.Text("Task add failed: "+err.Error()))
			return
		}
		bot.Reply(msg, pluginsdk.Text("Task added: "+task.ID+" next="+task.NextRunAt.Format(time.RFC3339)))
	case "del":
		if len(args) < 2 || !p.scheduler.Delete(args[1]) {
			bot.Reply(msg, pluginsdk.Text("Task not found."))
			return
		}
		bot.Reply(msg, pluginsdk.Text("Task deleted."))
	case "pause", "resume":
		if len(args) < 2 || !p.scheduler.SetEnabled(args[1], args[0] == "resume") {
			bot.Reply(msg, pluginsdk.Text("Task not found."))
			return
		}
		bot.Reply(msg, pluginsdk.Text("Task updated."))
	default:
		bot.Reply(msg, pluginsdk.Text("Usage: /agent task add|del|pause|resume ..."))
	}
}

func shouldHandleGroupMessage(msg *pluginsdk.Message, cfg *Config) bool {
	if !cfg.ChatTrigger.RequireMentionInGroup {
		return true
	}
	for _, seg := range msg.Segments {
		if seg.Type == "at" {
			return true
		}
	}
	return strings.Contains(strings.ToLower(msg.Text), "agent")
}

func main() {
	pluginsdk.Run(&AgentPlugin{})
}
