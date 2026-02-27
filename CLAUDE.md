# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**pureclaw** is a minimalist LLM agent written in Go, designed to run on very low-resource devices (Raspberry Pi, ≤1 GB RAM). Single binary, single LLM (Mistral), single interface (Telegram), file-based storage (Markdown/JSON on disk, no database).

## Build & Test Commands

```bash
# Build (static binary, no CGO)
CGO_ENABLED=0 go build -o pureclaw ./cmd/pureclaw

# Cross-compile for Raspberry Pi
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o pureclaw-arm64 ./cmd/pureclaw

# Run all tests with coverage
go test -cover ./...

# Single test
go test -run TestName ./path/to/package/...

# Coverage report
go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out
```

## Architecture

```
cmd/pureclaw/           # CLI entry point (init, run, vault, version)
internal/
  agent/                # Main agent loop: Telegram poll → context build → Mistral call → tool exec → respond
  config/               # config.json loading/saving (workspace path, models, heartbeat interval)
  vault/                # Encrypted keychain (AES-256-GCM + PBKDF2-SHA256 via x/crypto)
  mistral/              # Mistral API client (chat completions + audio transcription), raw net/http
  telegram/             # Telegram Bot API client (long polling, send message, file download)
  memory/               # File-based memory: write/read/search/compact (memory/YYYY/MM/DD/HH.md)
  workspace/            # Workspace file operations (read/write AGENT.md, SOUL.md, HEARTBEAT.md, skills)
  tools/                # Tool registry and execution (exec_command, read_file, write_file, list_dir, memory_*, spawn_agent)
  heartbeat/            # Periodic heartbeat: reads HEARTBEAT.md → sends to LLM → acts or stays silent
  subagent/             # Sub-agent spawning: create workspace, run isolated, collect result.md
```

### Core Flow

1. Telegram message received (long polling)
2. Context assembled: `SOUL.md` + `AGENT.md` + recent memory + relevant skills
3. Mistral API call with native function calling
4. If tool call → execute tool (or spawn sub-agent) → return result to LLM → loop
5. If text → send Telegram reply
6. Save interaction to hourly memory file

### Workspace (on-disk agent definition)

| File | Purpose |
|---|---|
| `AGENT.md` | Agent identity, tools, environment (introspection results) |
| `SOUL.md` | Personality, limits, communication style |
| `HEARTBEAT.md` | Checklist executed each heartbeat cycle |
| `skills/*/SKILL.md` | Specialized skills ([agentskills.io](https://agentskills.io) format) |
| `memory/YYYY/MM/DD/HH.md` | Hourly timestamped memory entries |
| `agents/<task-id>/` | Sub-agent isolated workspaces (depth=1 max) |

## Key Constraints

- **Single dependency**: only `golang.org/x/crypto` (for PBKDF2). Everything else is stdlib.
- **No CGO**: `CGO_ENABLED=0` always. Static binary.
- **100% test coverage** target. Use `testing` stdlib + `net/http/httptest` for HTTP mocks.
- **Go style**: use `any` (not `interface{}`), English for all code identifiers and comments.
- **Memory-conscious**: < 30 MB at rest. No full-file loading in RAM; read on demand.
- **HTTP timeouts**: 10s for Mistral API, retry with exponential backoff (max 3 attempts).
- **Goroutine budget**: main + Telegram poller + heartbeat ticker + at most 1 sub-agent.

## Sub-agents

The main agent can spawn sub-agents (`spawn_agent` tool) for delegated tasks:
- Sub-agent runs in isolated workspace under `agents/<task-id>/`
- No Telegram access (silent worker)
- Cannot spawn further sub-agents (max depth = 1)
- Configurable timeout (default 5 min)
- Writes result to `agents/<task-id>/result.md`

## CLI

```
pureclaw init                       # Interactive onboarding
pureclaw run                        # Start main agent
pureclaw run --agent agents/<id>    # Start sub-agent (internal use)
pureclaw vault get|set|delete|list  # Manage encrypted vault
pureclaw version                    # Print version
```

## Mistral API

- **Chat**: `POST /v1/chat/completions` with `mistral-large-latest`, function calling via `tools` field
- **Audio transcription**: `POST /v1/audio/transcriptions` with `voxtral-mini-latest` (Telegram voice messages)
- Auth: `Authorization: Bearer <key>` from vault
