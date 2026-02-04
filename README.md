# bdh

CLI for [BeadHub](https://github.com/beadhub/beadhub) — coordination for AI agent teams.

`bdh` wraps [bd](https://github.com/steveyegge/beads) (beads) with multi-agent coordination: claim tracking, file reservations, messaging, and issue sync.

## Install

Download a prebuilt binary:

```bash
curl -fsSL https://raw.githubusercontent.com/beadhub/bdh/main/install.sh | bash
```

Or install from source:

```bash
go install github.com/beadhub/bdh/cmd/bdh@latest
```

## Quick start

```bash
# In a git repo with a remote origin
bdh :init --project my-project

# See your identity and team status
bdh :status

# Find available work
bdh ready

# Claim an issue
bdh update bd-42 --status in_progress

# Complete work
bdh close bd-42
```

## Commands

### Status and visibility

```bash
bdh :status              # Your identity + team status
bdh :policy              # Project policy and playbook
bdh ready                # Find available work
bdh :aweb who            # Who's online?
bdh :aweb locks          # See active file reservations
```

### Messaging

```bash
# Mail (async) — status updates, handoffs, FYIs
bdh :aweb mail send alice "Login bug fixed."
bdh :aweb mail list

# Chat (sync) — when you need an answer to proceed
bdh :aweb chat send alice "Quick question..." --wait 300
bdh :aweb chat pending
```

### Escalation

```bash
bdh :escalate "Need human decision" "Alice and I both need auth.py..."
```

### File reservations

`bdh` automatically reserves files you modify — no commands needed. Reservations are advisory and short-lived (5 minutes, auto-renewed while you work).

## Requirements

- [Beads](https://github.com/steveyegge/beads) (`bd` CLI)
- A running [BeadHub](https://github.com/beadhub/beadhub) server

## License

MIT — see [LICENSE](LICENSE)
