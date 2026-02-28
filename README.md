```
                                     .__
  ______  __ _________   ____   ____ |  | _____  __  _  __
  \____ \|  |  \_  __ \_/ __ \_/ ___\|  | \__  \ \ \/ \/ /
  |  |_> >  |  /|  | \/\  ___/\  \___|  |__/ __ \_\     /
  |   __/|____/ |__|    \___  >\___  >____(____  / \/\_/
  |__|                      \/     \/          \/
```

# pureclaw

[![CI](https://github.com/edouard-claude/pureclaw/actions/workflows/ci.yml/badge.svg)](https://github.com/edouard-claude/pureclaw/actions/workflows/ci.yml)
[![Release](https://github.com/edouard-claude/pureclaw/actions/workflows/release.yml/badge.svg)](https://github.com/edouard-claude/pureclaw/actions/workflows/release.yml)
[![codecov](https://codecov.io/gh/edouard-claude/pureclaw/branch/master/graph/badge.svg)](https://codecov.io/gh/edouard-claude/pureclaw)
[![Go Report Card](https://goreportcard.com/badge/github.com/edouard-claude/pureclaw)](https://goreportcard.com/report/github.com/edouard-claude/pureclaw)
[![GitHub release](https://img.shields.io/github/v/release/edouard-claude/pureclaw?include_prereleases)](https://github.com/edouard-claude/pureclaw/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/edouard-claude/pureclaw)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**A self-contained LLM agent that fits in your pocket.**

Single binary. Zero database. Runs on a Raspberry Pi.

---

## Why

Modern LLM agents require 16 GB of RAM, Docker, Redis, PostgreSQL and three YAML files. pureclaw takes the opposite approach: **one Go binary**, one dependency (`x/crypto`), and it runs on a Pi with 1 GB of RAM.

## How it works

```
  Telegram ──poll──▶ Agent Loop ──api──▶ Mistral
      ◀──reply──       │    ▲              │
                       │    └──tool call───┘
                       ▼
                  .md files
                  (memory, skills, soul)
```

1. You talk to your Telegram bot (text or voice)
2. The agent loads its context (personality, memory, skills)
3. Mistral responds, with or without tool calls
4. Tools execute (shell, files, sub-agents)
5. The reply comes back on Telegram
6. Everything is saved as Markdown on disk

## Stack

| | |
|---|---|
| Language | Go 1.25, static binary, zero CGO |
| LLM | Mistral (`mistral-large-latest` + `voxtral-mini-latest` for voice) |
| Interface | Telegram Bot API (long polling, HTML formatting) |
| Storage | Markdown/JSON files on disk |
| Crypto | AES-256-GCM + PBKDF2-SHA256 (`golang.org/x/crypto`) |
| CI/CD | GitHub Actions + GoReleaser |
| Dependencies | **1** (just `golang.org/x/crypto`) |

## Install

### From releases

Download the latest binary from [Releases](https://github.com/edouard-claude/pureclaw/releases):

```bash
# Linux amd64
curl -L https://github.com/edouard-claude/pureclaw/releases/latest/download/pureclaw_linux_amd64.tar.gz | tar xz

# Raspberry Pi (armv7)
curl -L https://github.com/edouard-claude/pureclaw/releases/latest/download/pureclaw_linux_armv7.tar.gz | tar xz

# macOS arm64
curl -L https://github.com/edouard-claude/pureclaw/releases/latest/download/pureclaw_darwin_arm64.tar.gz | tar xz
```

### From source

```bash
# Local build
make build

# Raspberry Pi (armv7l)
make build-pi

# Linux arm64
make build-arm64

# With version tag
make build VERSION=1.0.0
```

## Quickstart

### Init

The interactive wizard asks for:
- Your Mistral API key
- Your Telegram Bot token
- Allowed Telegram user IDs (strict whitelist)
- A passphrase for the encrypted vault
- Heartbeat interval (default: 30 min)

```bash
./pureclaw init
```

This creates `config.json`, `vault.enc`, and the workspace with default files.

### Run

```bash
./pureclaw run                          # Main agent
./pureclaw run --agent agents/<id>      # Sub-agent (internal use)
```

### Deploy to a Pi

```bash
make build-pi
scp pureclaw-arm7 kali@raspberrypi:~/pureclaw
ssh kali@raspberrypi "chmod +x ~/pureclaw && ~/pureclaw init"
```

### Vault

```bash
./pureclaw vault list                   # List keys
./pureclaw vault get telegram.token     # Read a key
./pureclaw vault set mistral.api_key    # Write a key
./pureclaw vault delete old.key         # Delete a key
```

## Architecture

```
cmd/pureclaw/           CLI (init, run, vault, version)
internal/
  agent/                Main loop: poll → context → LLM → tools → respond
  config/               config.json loading/saving
  vault/                Encrypted keychain (AES-256-GCM + PBKDF2)
  llm/                  Mistral API client (chat + audio transcription)
  telegram/             Telegram Bot API client (polling, send, file download)
  memory/               File-based memory: write/read/search/compact
  workspace/            Workspace file ops (AGENT.md, SOUL.md, HEARTBEAT.md, skills)
  tool/                 Tool registry (exec_command, read/write_file, list_dir, spawn_agent...)
  heartbeat/            Periodic heartbeat: reads HEARTBEAT.md → LLM → act or stay silent
  subagent/             Sub-agent spawning: isolated workspace, collect result.md
  platform/             Low-level utils (atomic write, retry, path guard)
  watcher/              File watcher
```

### On-disk workspace

```
~/.pureclaw/workspace/
├── AGENT.md              # Identity, tools, environment
├── SOUL.md               # Personality and style
├── HEARTBEAT.md          # Heartbeat checklist
├── skills/
│   └── */SKILL.md        # Specialized skills
├── memory/
│   └── YYYY/MM/DD/HH.md # Hourly timestamped memory
└── agents/
    └── <task-id>/        # Isolated sub-agents
        ├── AGENT.md
        ├── SOUL.md
        └── result.md
```

## Built-in tools

| Tool | Description |
|---|---|
| `exec_command` | Run a shell command |
| `read_file` | Read a file |
| `write_file` | Write a file |
| `list_dir` | List a directory |
| `memory_search` | Search through memory files |
| `memory_write` | Write a memory entry |
| `spawn_agent` | Delegate a task to a sub-agent |
| `reload_workspace` | Reload workspace files |

## Tests

```bash
make test                               # Tests + coverage
make coverage                           # Coverage report
make vet                                # Static analysis
go test -run TestVaultRoundTrip ./...   # Specific test
```

## Releasing

Releases are automated via GitHub Actions + GoReleaser. To create a release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

GoReleaser builds binaries for linux/darwin (amd64, arm64, armv7), creates a GitHub Release with checksums and changelog.

## Design constraints

- **< 30 MB** RAM at rest
- **4 goroutines** max: main + Telegram poller + heartbeat + 1 sub-agent
- **30s timeout** on Mistral API calls, exponential backoff retry (max 3)
- **No streaming** (unnecessary for Telegram)
- Sub-agents: max depth 1, no Telegram access, 5 min timeout

## What pureclaw is NOT

- Not multi-LLM
- Not multi-interface
- No browser automation
- No database
- No web GUI
- No MCP server

**A minimal, autonomous personal agent that runs on the smallest hardware possible.**

## License

MIT
