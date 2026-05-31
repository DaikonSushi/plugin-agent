# plugin-agent

Personal AI agent plugin for `bot-platform`.

## Features

- OpenAI-compatible chat completions with tool calling
- Local project awareness through file listing, reading, ripgrep search, and project indexing
- Tailscale/SSH access through allowlisted remote hosts
- HTTP requests to allowlisted endpoints
- Scheduled reminders and recurring agent tasks
- Skill loading from `plugins-config/agent/skills/<name>/SKILL.md`
- JSON state, conversation, task, and audit persistence

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

## Local Build

```bash
go test ./...
go build -ldflags="-s -w" -o agent-plugin .
./agent-plugin --info
```

For local development inside the monorepo, `go.mod` keeps a local `replace` directive for `../bot-platform`. The release workflow removes it before building.

## Install

After a GitHub release is created:

```text
/plugin install https://github.com/DaikonSushi/plugin-agent
/plugin start agent
```
