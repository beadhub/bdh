# bdh — Source of Truth

## What This Is

Go CLI that wraps `bd` (beads). All `bd` commands pass through unchanged; `bdh` adds coordination transparently — notifying the BeadHub server what you're doing, syncing beads state, and providing protocol commands (`:aweb mail`, `:aweb chat`, `:status`, `:policy`, etc.).

## Stack

- **Language**: Go
- **Dependencies**: Imports `aw` library for aweb protocol communication (mail, chat, locks, presence)
- **Build**: `go build ./cmd/bdh`
- **Test**: `go test ./...`

## Ecosystem Role

Client-side tool. Talks to the BeadHub server API over HTTP. Identity derived from the `.beadhub` file in the current git worktree. Each worktree = one agent identity.

## Key Architecture

- `cmd/bdh/` — Entry point
- `internal/commands/` — Command routing. Commands prefixed with `:` are bdh-specific (`:status`, `:policy`, `:aweb`, `:init`). Everything else is passed through to `bd`.
- `internal/client/` — HTTP client for the BeadHub server API
- `internal/config/` — Configuration loading from `.beadhub` file
- Policy caching: 60s TTL in `.beadhub-cache/` to avoid hitting the server on every `bdh` invocation

## Run Config Behavior

- `bdh :run` uses `aw` run config as the generic base (`~/.config/aw/run.json` + local `.aw/run.json`) and then applies bdh overlays (`~/.config/beadhub/run.json` + local `.beadhub-run.json`).
- Malformed `aw` run config is a fail-fast error by design (parity with `aw`), not a silently ignored condition.

## Release

GoReleaser on git tags. Workflow: `.github/workflows/bdh-release.yml`. Produces binaries for multiple platforms.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/beadhub/bdh/main/install.sh | bash
```
