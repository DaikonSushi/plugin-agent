# plugin-agent

Personal AI agent plugin for `bot-platform`.

## Features

- OpenAI-compatible chat completions with tool calling
- Local project awareness through file listing, reading, ripgrep search, and project indexing
- Coding-agent tools for allowlisted file writes, Go tests/builds/formatting, and git workflows
- Bot runtime awareness through `runtime_status`
- Online plugin lifecycle operations through `plugin_control`
- Tailscale/SSH access through allowlisted remote hosts
- HTTP requests to allowlisted endpoints
- Scheduled reminders and recurring agent tasks
- Skill loading from `plugins-config/agent/skills/<name>/SKILL.md`
- A default `qq_bot_platform` skill is created on first start
- JSON state, conversation, task, and audit persistence

The agent declares itself as a low-priority fallback plugin:

```json
{
  "handle_all_messages": true,
  "message_priority": -1000,
  "fallback": true
}
```

This lets domain plugins claim keyword messages before the agent sees them.

## Commands

```text
/ai <prompt>
/agent on
/agent off
/agent status
/agent tasks
/agent task add <schedule> <prompt>
/agent task del <id>
/agent task pause <id>
/agent task resume <id>
/agent skills
/agent index
/agent model status
/agent model hermes [base_url] [api_key_env]
/agent model openai [base_url] [model] [api_key_env]
/agent panic
```

Schedules support:

```text
10m
2h
once:2026-06-01 09:30
daily@09:30
weekly@Mon,09:30
```

## Configuration

On first start, the plugin creates:

```text
plugins-config/agent/config.json
plugins-config/agent/data/state.json
```

Set the model API key with the configured environment variable. The default is:

```bash
export OPENAI_API_KEY=...
```

The default model endpoint is OpenAI-compatible:

```json
{
  "model": {
    "base_url": "https://api.openai.com/v1",
    "api_key_env": "OPENAI_API_KEY",
    "model": "gpt-4.1-mini"
  }
}
```

### Hermes Agent Gateway

Hermes Agent exposes an OpenAI-compatible API server. Start Hermes with its API server enabled, then point this plugin at the gateway:

```bash
export HERMES_API_SERVER_KEY=change-me-local-dev
hermes gateway
```

```text
/agent model hermes http://127.0.0.1:8642/v1 HERMES_API_SERVER_KEY
/agent model status
```

Hermes mode uses:

```json
{
  "provider": "hermes",
  "base_url": "http://127.0.0.1:8642/v1",
  "api_key_env": "HERMES_API_SERVER_KEY",
  "model": "hermes-agent",
  "disable_local_tools": true,
  "hermes_session": true
}
```

When `hermes_session` is enabled, each QQ chat gets an `X-Hermes-Session-Key` header so Hermes can keep its own session continuity. Local plugin tools are disabled in Hermes mode because Hermes is itself the agent runtime.

Access control supports account, group, and group-role whitelists. Empty lists allow all.

```json
{
  "access": {
    "person_whitelist": [493541311],
    "group_whitelist": [649219772, 787632139],
    "role_whitelist": ["owner", "admin"]
  },
  "admin_users": [2577954317]
}
```

## Local Build

```bash
go test ./...
go build -ldflags="-s -w" -o agent-plugin .
./agent-plugin --info
```

For local development inside the monorepo, `go.mod` keeps a local `replace` directive for `../bot-platform`. CI and release workflows checkout `bot-platform` beside this repository so SDK compatibility is tested against the platform source.

## CI Gates

The repository includes GitHub Actions checks for serious plugin development:

- gofmt must be clean
- `go test ./...` must pass
- `go build` must produce the plugin binary
- `./agent-plugin --info` must return valid metadata
- agent fallback metadata must remain enabled

Release builds run from GitHub Actions on `v*` tags and upload installable binaries.

## Install

After a GitHub release is created:

```text
/plugin install https://github.com/DaikonSushi/plugin-agent
/plugin start agent
```
