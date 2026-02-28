# cmd/pureclaw CLAUDE.md

## CLI Entry Point

`main.go` dispatches subcommands: `init`, `run`, `vault`, `version`.

- `Version` variable is set at build time via `-ldflags "-X main.Version=x.y.z"`. Defaults to `"dev"`.
- `run.go` is the main agent startup: loads config, reads vault passphrase, creates all clients, starts event loop.
- `run_subagent.go` handles `run --agent <path>` for isolated sub-agent execution.

## Vault Passphrase

Two modes:
1. **Interactive**: reads from stdin (terminal prompt)
2. **Env var**: `PURECLAW_VAULT_PASSPHRASE` — used by systemd service (`/etc/pureclaw.env`)

Both `run.go` and `run_subagent.go` support this.

## Testing

Tests use replaceable package-level vars for all dependencies (config loader, vault, LLM client, Telegram client, etc.). Pattern:

```go
var configLoad = config.Load          // replaceable for tests
var newLLMClient = func(...) { ... }  // stub in test
```

`saveRunVars(t)` saves and restores all vars via `t.Cleanup()`.

## CI/CD

### Workflows (`.github/workflows/`)

| Workflow | Trigger | What it does |
|---|---|---|
| `ci.yml` | push/PR to master | `go vet`, `go test -race` with coverage, multi-arch build matrix, codecov upload |
| `release.yml` | tag push `v*` | GoReleaser builds + GitHub Release with binaries and checksums |

### Releasing

```bash
git tag v0.1.0
git push origin v0.1.0
# GitHub Actions runs goreleaser → builds linux/darwin (amd64, arm64, armv7) → creates release
```

### GoReleaser config (`.goreleaser.yml`)

- Builds: `linux/{amd64,arm64,armv7}`, `darwin/{amd64,arm64}`
- ldflags: `-s -w -X main.Version={{.Version}}`
- Archives: `tar.gz` with arch suffix
- Checksums: `checksums.txt`
- Changelog: excludes `docs:`, `test:`, `ci:`, `chore:` prefixes

### Tag format

Semver with `v` prefix: `v0.1.0`, `v1.0.0`, `v1.2.3-beta.1`.

### Required secrets

| Secret | Where | Purpose |
|---|---|---|
| `GITHUB_TOKEN` | auto-provided | GoReleaser creates releases |
| `CODECOV_TOKEN` | repo settings | Coverage upload (optional) |

## Makefile targets

```bash
make build              # Local binary (CGO_ENABLED=0)
make build-pi           # Raspberry Pi armv7
make build-arm64        # Linux arm64
make test               # Tests with race detector + coverage
make coverage           # Print coverage report
make vet                # Static analysis
make release-dry        # GoReleaser dry run (snapshot)
make clean              # Remove binaries and coverage files
```

All build targets accept `VERSION=x.y.z` to set the version string.
