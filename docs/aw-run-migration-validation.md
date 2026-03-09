# aw-backed `bdh :run` migration validation

Date: 2026-03-09
Branch: `noah`

## Automated regression coverage

Focused runtime/parity subset:

```bash
go test ./internal/commands -run 'Test(SelectRunDispatch|RunLoopComposesBasePromptWithDispatcherPrompt|RunLoopWaitsForDispatchRecoveryOnDispatchErrorEvenWithBasePrompt|RunLoopWaitsForDispatchRecoveryOnDispatchErrorWithoutBasePrompt|AwRunDispatcherAdapterIntegratesWithAwLoop|AwRunDispatcherAdapterMapsDecision|AwRunDispatcherAdapterPropagatesError|RunEventStreamClientStream|RunEventStreamClientStreamHTTPError|RunLoopReconnectsWakeStreamAfterEarlyClose|RunServiceManager|RunServicesPromptSection|ClaudeProvider|CodexProvider|ResolveRunDebugMode)' -count=1
```

Full suite:

```bash
go test ./...
```

Status: pass

## Manual smoke validation

Both default aw path and `--debug` fallback path were exercised end-to-end using a temporary local `codex` stub on `PATH` that emits minimal JSON events (`thread.started`, `turn.completed`) and exits `0`.

Commands executed:

```bash
PATH="$TMPBIN:$PATH" go run ./cmd/bdh :run --provider codex --max-runs 1 --wait 0 --idle-wait 0 "manual default-path smoke"
PATH="$TMPBIN:$PATH" go run ./cmd/bdh :run --provider codex --debug --max-runs 1 --wait 0 --idle-wait 0 "manual debug-path smoke"
```

Observed behavior:

- Both runs exited cleanly (`exit 0`)
- Both runs rendered expected run header/provider mode/session/done output
- Both runs respected `--max-runs 1` and terminated with `done: reached max-runs (1)`

## Boundary confirmation

- Default (non-debug) path uses aw-owned runtime primitives:
  - aw loop/provider
  - aw wake stream (`NewClientWakeStream`)
  - aw screen controller
  - aw service supervisor
- bdh-owned behavior retained:
  - bead-aware dispatch prioritization and prompt shaping
  - autofeed/coordination policy semantics
- Remaining legacy runtime surfaces are confined to `--debug` fallback.
