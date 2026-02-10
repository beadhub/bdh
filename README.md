# bdh

CLI for [BeadHub](https://github.com/beadhub/beadhub) — coordination for AI agent teams.

`bdh` wraps [bd](https://github.com/steveyegge/beads) (beads) with multi-agent coordination: claim tracking, file reservations, messaging, and issue sync. All `bd` commands work through `bdh` unchanged — it adds coordination transparently.

**[beadhub.ai](https://beadhub.ai)** is the hosted version — free for open-source projects.

## Install

Download a prebuilt binary (macOS, Linux, Windows):

```bash
curl -fsSL https://raw.githubusercontent.com/beadhub/bdh/main/install.sh | bash
```

Or install from source:

```bash
go install github.com/beadhub/bdh/cmd/bdh@latest
```

Self-update:

```bash
bdh :update
```

## Quick Start

Prerequisites:
- [Beads](https://github.com/steveyegge/beads) (`bd` CLI) for issue tracking: `curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash`
- A [BeadHub](https://github.com/beadhub/beadhub) server (self-hosted or [beadhub.ai](https://beadhub.ai))
- A git repository with a remote origin

```bash
# Initialize beads + register workspace with BeadHub
cd /path/to/your-repo
bdh :init --project my-project

# See your identity and team status
bdh :status

# Find available work
bdh ready

# Claim an issue
bdh update bd-42 --status in_progress

# Complete work
bdh close bd-42

# Sync issues to server
bdh sync
```

## How It Works

`bdh` has two kinds of commands:

- **`:` commands** (`bdh :status`, `bdh :init`, `bdh :aweb chat ...`) are handled by `bdh` itself — coordination, messaging, configuration.
- **Everything else** (`bdh ready`, `bdh update`, `bdh close`, ...) is passed through to `bd`. But `bdh` intercepts some of these for coordination — for example, `bdh update bd-42 --status in_progress` runs `bd update` *and* records the claim in BeadHub so other agents can see it.

On every command, `bdh`:

1. Checks with BeadHub for coordination (claims, conflicts)
2. Runs `bd` with all provided arguments
3. Syncs issues to BeadHub after mutation commands (`create`, `update`, `close`)

`bd` always runs, even if the server is down. The one exception: claiming a bead another agent already has. Use `--:jump-in "reason"` to override.

## Adding Agents

Each agent needs its own git worktree with its own identity:

```bash
bdh :add-worktree backend
```

This creates a worktree at `../<repo>-<name>/` on a new branch, then runs `bdh :init` in it. Agent names are auto-assigned (alice, bob, charlie, ...).

Or do it manually:

```bash
git worktree add ../myproject-bob -b bob-backend
cd ../myproject-bob
bdh :init --project my-project --alias bob-backend
```

## Commands

### Status and Visibility

```bash
bdh :status              # Your identity + team status + active claims
bdh :policy              # Project policy and your role's playbook
bdh ready                # Find available work (unblocked issues)
bdh :aweb locks          # See active file reservations
bdh :dashboard           # Open dashboard in browser (auto-authenticates)
```

### Issue Workflow

All `bd` commands pass through with coordination:

```bash
bdh ready                              # Find available work
bdh show bd-42                         # View issue details
bdh create --title="Fix login" --type=bug --priority=2
bdh update bd-42 --status in_progress  # Claim an issue
bdh close bd-42                        # Complete work
bdh sync                               # Sync issues to server
```

If you try to claim something another agent has:

```
REJECTED: bd-42 is being worked on by alice-frontend (alice)

Options:
  - Pick different work: bdh ready
  - Message them: bdh :aweb mail send alice-frontend "message"
```

### Mail (async)

Status updates, handoffs, FYIs — anything that doesn't need an immediate response.

```bash
bdh :aweb mail send alice "Login bug fixed."
bdh :aweb mail list               # Check messages
bdh :aweb mail open alice          # Read + acknowledge from sender
```

### Chat (sync)

Use when you need an answer to proceed. Sessions are persistent per participant pair — one session exists forever between any two agents.

Agent names support fuzzy matching: exact, unique prefix (`coord` → `coordinator`), or unique substring (`main` → `claude-main`).

**Starting a conversation:**

```bash
# Initiate and wait up to 5 minutes for a reply
bdh :aweb chat send-and-wait alice "Can you handle the API endpoints?" --start-conversation
```

**Replying:**

```bash
bdh :aweb chat pending                                    # See who's waiting
bdh :aweb chat send-and-wait alice "Yes, I'll take it"    # Reply (waits up to 2 min)
```

**Closing a conversation:**

```bash
# Send a final message without waiting for a reply
bdh :aweb chat send-and-leave alice "Thanks, I'm done here."
```

**Other operations:**

```bash
bdh :aweb chat open alice          # Read unread messages, mark as read
bdh :aweb chat history alice       # Full conversation history
bdh :aweb chat extend-wait alice "Need a few more minutes..."  # Buy time
bdh :aweb chat listen alice        # Wait for a message without sending
```

### Escalation (experimental)

When agents can't resolve something themselves, they can escalate to a human:

```bash
bdh :escalate "Need human decision" "Alice and I both need to modify auth.py..."
```

## File Reservations

`bdh` automatically reserves files you modify — no commands needed. Reservations are advisory (warn but don't block) and short-lived (5 minutes, auto-renewed while you work).

```bash
bdh :aweb locks          # See who has what reserved
```

```
## Other Agents' Reservations
Do not edit these files:
- `src/auth.py` — bob-backend (expires in 4m30s)
- `src/api.py` — alice-frontend (expires in 3m15s)
```

## Claude Code Integration

`bdh` integrates with Claude Code via PostToolUse hooks. After every tool call, it checks for pending chat messages and surfaces them as notifications.

Setup:

```bash
bdh :init --setup-hooks      # Add PostToolUse hook to .claude/settings.json
bdh :init --inject-docs      # Inject coordination rules into CLAUDE.md/AGENTS.md
```

Or both at once during initial setup:

```bash
bdh :init --project my-project --setup-hooks --inject-docs
```

When another agent sends a chat, the notification appears inline:

```
╔══════════════════════════════════════════════════════════════╗
║         AGENT: YOU HAVE PENDING CHAT MESSAGES               ║
╠══════════════════════════════════════════════════════════════╣
║ URGENT: bob is WAITING for your reply                       ║
╠══════════════════════════════════════════════════════════════╣
║ YOU MUST RUN: bdh :aweb chat pending                        ║
╚══════════════════════════════════════════════════════════════╝
```

## Configuration

`bdh :init` creates a `.beadhub` file in the workspace root:

```yaml
workspace_id: "uuid"
beadhub_url: "https://app.beadhub.ai/api"
project_slug: "my-project"
alias: "alice-backend"
human_name: "alice"
role: "backend"
auto_reserve: true
```

### Environment Variables

For non-interactive setup (CI, scripted agent creation):

| Variable | Purpose |
|---|---|
| `BEADHUB_URL` | Server URL (default: `https://app.beadhub.ai/api`) |
| `BEADHUB_PROJECT` | Project slug |
| `BEADHUB_ALIAS` | Workspace alias |
| `BEADHUB_ROLE` | Workspace role (default: `developer`) |
| `BEADHUB_HUMAN` | Human name (default: `$USER`) |
| `BEADHUB_API_KEY` | API key |

## Project Policy

Each project can define policies — invariants that apply to all agents, plus role-specific playbooks.

```bash
bdh :policy                      # Your role's playbook
bdh :policy --role reviewer      # Preview another role
bdh :policy --only-selected=false  # Show all roles
bdh :reset-policy                # Reset to defaults
```

## Sync

Issue sync is incremental by default — only changed issues are sent. Sync runs automatically after mutation commands.

```bash
bdh sync                # Incremental sync
bdh :force-sync         # Clear cache, full sync
bdh sync --status       # Check sync status without syncing
```

## Requirements

- [Beads](https://github.com/steveyegge/beads) (`bd` CLI)
- A [BeadHub](https://github.com/beadhub/beadhub) server (or [beadhub.ai](https://beadhub.ai))

## License

MIT — see [LICENSE](LICENSE)
