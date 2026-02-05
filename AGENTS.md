# Agent Instructions

This project uses `bdh` for multi-agent coordination and `bdh` (beads) for issue tracking. Start with `bdh :policy`.

## Quick Reference

```bash
bdh ready              # Find available work
bdh show <id>          # View issue details
bdh update <id> --status in_progress  # Claim work
bdh close <id>         # Complete work
bdh sync               # Sync with git
bdh :aweb mail send <alias> "message" # Async message
bdh :aweb chat send <alias> "message" --wait 60 # Sync chat
```

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bdh sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

<!-- BEADHUB:START -->
## BeadHub Coordination

This project uses `bdh` for multi-agent coordination and issue tracking.

**Start every session:**
```bash
bdh :policy    # READ CAREFULLY and follow diligently, start here now
bdh :status    # your identity + team status
bdh ready      # find unblocked work
bdh --help     # command reference
```

**Key rules:**
- Use `bdh` (not `bd`) so work is coordinated
- Default to mail (`bdh :aweb mail send <alias> "message"`); use chat (`bdh :aweb chat`) when blocked
- Respond immediately to WAITING notifications
- Prioritize good communication — your goal is for the team to succeed
- Before saying "done", follow the session close protocol in `bdh :policy` (includes `git push`)
<!-- BEADHUB:END -->