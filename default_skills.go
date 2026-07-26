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
7. For serious plugin development, create or use a remote Git repository as the source of truth. Do not treat only the running container filesystem as durable project state.
8. Commit coherent changes to a branch before deployment. Use tags/releases for installable external plugin binaries.
9. Do not rely on the model's memory of development rules alone. The repository should enforce rules with GitHub Actions or another CI system.
10. A plugin PR or release workflow should fail unless formatting, tests, build, and plugin metadata checks pass.

## Git And SSH Development

When the agent develops plugins, it should work from a Git checkout inside the configured workspace or a container sandbox.

Preferred GitHub access:

- Use SSH with a dedicated key or repository deploy key.
- Prefer read/write access only for the specific plugin repository the agent is allowed to modify.
- Mount the private key into the container as read-only, usually under /root/.ssh/id_ed25519 or a configured IdentityFile.
- Keep key file permissions strict: chmod 600 for private keys and chmod 700 for ~/.ssh.
- Pre-populate known_hosts with GitHub host keys using ssh-keyscan github.com, or mount a reviewed known_hosts file.
- Set GIT_SSH_COMMAND when a specific key or known_hosts path is needed.
- Do not disable host key checking except for temporary diagnostics.
- Do not store private keys, API keys, or tokens in skill files, plugin source, logs, or committed config.
- Verify remotes with git remote -v before pushing.
- Use git status and git diff before commits, and avoid overwriting unrelated user changes.

For plugin releases, prefer this flow:

1. create or update a branch
2. edit code
3. run go test ./...
4. run go build and ./plugin --info
5. commit changes
6. push branch or tag
7. create a GitHub release with the linux_amd64 binary
8. install or update through /plugin install or the plugin_control tool

## CI And Release Gates

Every serious plugin repository should include CI checks that enforce the development contract:

- gofmt must be clean
- go test ./... must pass
- go build must produce the expected plugin binary
- ./plugin --info must return valid JSON
- required metadata must exist: name, version, description, author, commands
- catch-all agent plugins must set handle_all_messages true, fallback true, and negative message_priority
- go.mod and go.sum must not be changed by CI commands

Release should usually be automated by GitHub Actions on version tags. The workflow should upload linux_amd64 release assets because bot-platform installs external plugins from GitHub releases.

The development container should include gh so the agent can inspect GitHub Actions and releases:

- gh auth status
- gh run list --repo owner/repo
- gh run view <run-id> --log
- gh release list --repo owner/repo
- gh release view <tag> --repo owner/repo

## External Plugin Contract

Use pluginsdk.PluginInfo with name, version, description, author, commands, handle_all_messages, message_priority, and fallback.

OnCommand handles slash commands registered in Commands.
OnMessage handles non-command messages. Return true only when the plugin has claimed the message.

For normal domain plugins, leave fallback false and use message_priority 0 unless they need to run earlier.
For catch-all assistant plugins, set handle_all_messages true, fallback true, and a low message_priority such as -1000.

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

## Message Pipeline

bot-platform dispatches external OnMessage handlers as a lightweight pipeline:

1. non-fallback plugins first
2. higher message_priority first
3. fallback plugins last

If a plugin returns handled=true, later plugins are skipped. This preserves keyword plugins such as showmejm while still allowing agent to answer as the final fallback.

## Online Plugin Operations

Installed external plugins are managed through the admin API:

- GET /api/plugins
- POST /api/plugins/install {"repo_url":"https://github.com/owner/repo","auto_start":true}
- POST /api/plugins/start {"name":"plugin"}
- POST /api/plugins/stop {"name":"plugin"}
- POST /api/plugins/uninstall {"name":"plugin"}

The agent should use bot_plugins and plugin_control tools for these operations instead of editing plugin metadata files directly.
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
