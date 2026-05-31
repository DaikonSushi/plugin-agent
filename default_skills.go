package main

import (
	"os"
	"path/filepath"
)

const qqBotSkill = `name: qq_bot_platform
description: Develop, test, deploy, and debug plugins for this QQ bot platform.
triggers: qq_bot, bot_platform, bot-platform, napcat, onebot, plugin, qq bot, agent plugin

# QQ Bot Platform Development Skill

Use this skill when the user asks to add, modify, debug, deploy, or test functionality in this QQ bot stack.

## Architecture

- NapCat handles QQ login and OneBot HTTP/WebSocket APIs.
- bot-platform connects to NapCat, routes messages, manages permissions, and manages plugins.
- External plugins are Go binaries that implement github.com/DaikonSushi/bot-platform/pkg/pluginsdk and connect back to bot-platform over gRPC.
- External plugins expose Info, OnStart, OnStop, OnMessage, and OnCommand.

## Repository Layout

- bot-platform: core platform, plugin manager, SDK, Docker deployment.
- plugin-agent: this personal AI agent plugin.
- plugin-showmejm: JM plugin with config, whitelist, auto message handling, downloads, and file upload.
- plugin-fileupload: file upload examples.
- plugin-echo: minimal external plugin example.

## Development Rules

1. Inspect existing code before editing.
2. Prefer existing patterns in nearby plugins.
3. Keep changes scoped and avoid unrelated refactors.
4. For external plugins, run go test ./..., go build, and ./<binary> --info.
5. For bot-platform changes, run go test ./...
6. If behavior depends on QQ/NapCat, check runtime logs and ask the user to verify via QQ commands when direct QQ testing is unavailable.

## External Plugin Contract

Use pluginsdk.PluginInfo with name, version, description, author, commands, and handle_all_messages.

OnCommand handles slash commands registered in Commands.
OnMessage handles non-command messages. Return true only when the plugin has claimed the message.

Useful SDK calls:

- bot.Reply(msg, pluginsdk.Text("message"))
- bot.SendGroupMessage(groupID, pluginsdk.Text("message"))
- bot.SendPrivateMessage(userID, pluginsdk.Text("message"))
- bot.UploadGroupFile(groupID, path, name, folder)
- bot.UploadPrivateFile(userID, path, name)
- bot.CallAPI(action, params)

## Runtime And Deployment

Usual all-in-one deployment uses compose.all-in-one.yaml from the workspace root.

Important paths inside the all-in-one container:

- /app/bot-platform/config/config.yaml
- /app/bot-platform/plugins-bin
- /app/bot-platform/plugins-config
- /app/plugins-config
- /shared-data

Useful checks:

- podman compose --env-file all-in-one.env.example -f compose.all-in-one.yaml ps
- podman compose --env-file all-in-one.env.example -f compose.all-in-one.yaml logs -f qq-bot
- /plugin list
- /help

## Agent Plugin Guidance

When adding capabilities to plugin-agent:

- Add commands in Info only when users should call them directly.
- Prefer tools for model-driven actions and commands for deterministic controls.
- Gate filesystem, command, SSH, and HTTP tools through allowlists.
- Add tests for config parsing, permissions, schedule parsing, and tool behavior.
- Update README and bump plugin version before release.

## Message Priority

bot-platform should let domain plugins claim messages before agent fallback. For catch-all agent behavior, agent must run after plugins such as showmejm. If a domain plugin returns handled=true, agent should not run for that message.
`

func EnsureDefaultSkills(skillsDir string) error {
	path := filepath.Join(skillsDir, "qq_bot_platform", "SKILL.md")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(qqBotSkill), 0644)
}
