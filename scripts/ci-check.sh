#!/usr/bin/env bash
set -euo pipefail

if [ ! -f go.mod ]; then
  echo "ci-check must run from the plugin repository root" >&2
  exit 1
fi

if [ ! -d ../bot-platform ]; then
  echo "../bot-platform is required because go.mod uses a local replace for SDK compatibility" >&2
  exit 1
fi

changed="$(gofmt -l .)"
if [ -n "${changed}" ]; then
  echo "Go files need gofmt:" >&2
  echo "${changed}" >&2
  exit 1
fi

go test ./...

go build -ldflags="-s -w" -o /tmp/agent-plugin_linux_amd64 .
info="$(/tmp/agent-plugin_linux_amd64 --info)"

INFO_JSON="${info}" python3 - <<'PY'
import json
import os
import sys

info = json.loads(os.environ["INFO_JSON"])
required = ["name", "version", "description", "author", "commands"]
missing = [key for key in required if not info.get(key)]
if missing:
    print(f"missing required plugin info fields: {missing}", file=sys.stderr)
    sys.exit(1)
if info["name"] != "agent":
    print(f"unexpected plugin name: {info['name']}", file=sys.stderr)
    sys.exit(1)
if not info.get("handle_all_messages"):
    print("agent must handle all messages", file=sys.stderr)
    sys.exit(1)
if not info.get("fallback"):
    print("agent must be marked as fallback", file=sys.stderr)
    sys.exit(1)
if info.get("message_priority", 0) >= 0:
    print("agent message_priority must be negative", file=sys.stderr)
    sys.exit(1)
PY

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git diff --exit-code -- go.mod go.sum
fi
