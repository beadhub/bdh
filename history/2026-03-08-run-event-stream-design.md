# bdh :run Event-Driven Wakeup Design

Date: 2026-03-08
Author: noah
Related bead: `beadhub-034`

## Goal

Replace the idle polling model in `bdh :run` with an event-driven wakeup model built on a unified per-agent SSE stream, while keeping snapshot fetches as the source of truth for dispatch decisions.

This is intended to work for:

- legacy BeadHub
- new aweb-based products

The event stream should live in aweb OSS so both platforms can consume the same coordination primitive.

## Current State

Today `bdh :run` is poll-driven.

On each cycle, dispatch checks:

1. pending chat
2. unread mail
3. current claim
4. ready work
5. otherwise wait and poll again

This has three drawbacks:

1. Idle loops wake up even when nothing changed.
2. Human interruption is indirect and delayed.
3. Control semantics such as "pause all my agents now" do not have a first-class transport.

There is already a narrow SSE-style capability in `bdh`:

- `bdh :aweb chat listen <alias>`

That proves the stack can support stream-based waiting, but it is too narrow for `:run` because it is scoped to one existing conversation, not the agent as a whole.

## Core Decision

Use a unified per-agent event stream only for wakeup and control.

Do not use the stream itself as the full state source.

When an event arrives, `:run` should fetch fresh state and then make a normal dispatch decision. This keeps the system robust across reconnects, dropped events, duplicate events, and race conditions.

In short:

- events wake
- snapshots decide

## Proposed Endpoint

`GET /v1/events/stream`

Properties:

- scoped to the authenticated agent
- long-lived SSE connection
- minimal event payloads
- usable by legacy BeadHub and new aweb consumers

Agent identity should come from auth, not from a user-supplied query parameter.

## Event Types

Initial event types:

- `chat_message`
- `mail_message`
- `control_pause`
- `control_resume`
- `control_interrupt`

Deferred / optional:

- `work_available`

Event payloads should stay minimal. They are wake signals, not full snapshots.

Examples:

```json
{ "type": "chat_message", "sender_alias": "grace" }
{ "type": "mail_message", "sender_alias": "mia", "priority": "urgent" }
{ "type": "control_interrupt", "scope": "all_agents_for_human" }
```

## Why Not Parse Free Text Like "URGENT"

We should not overload message body text for control semantics.

Reasons:

- brittle and ambiguous
- hard to evolve
- impossible to reason about safely across products

Instead, urgency and control should be structured:

- message priority
- explicit control events
- optional interrupt metadata

## Client Model for `bdh :run`

### Idle Behavior

When `:run` is idle, it should block on the SSE stream instead of doing the current countdown-based poll loop.

Pseudo-flow:

1. Connect to `/v1/events/stream`
2. Wait for event
3. On event, fetch fresh state
4. Run normal dispatch logic
5. If no actionable state is found, go back to waiting

### Active Run Behavior

While an agent run is active, incoming events should not all be treated equally.

Default behavior:

- `chat_message` and `mail_message` queue a wakeup for the next cycle

Explicit control behavior:

- `control_pause`: finish current run, then pause
- `control_resume`: clear paused state
- `control_interrupt`: stop the current run immediately and switch to coordination

This preserves momentum for normal work while still enabling higher-priority human control.

### Snapshot Fetch After Wake

After wake, `:run` should fetch the same state it already uses today:

- pending chat
- unread mail
- current claims
- ready work

This keeps one dispatch policy regardless of whether wakeup came from polling or SSE.

## Reliability Requirements

The SSE client should not be the only correctness mechanism.

`bdh :run` should still do periodic fallback resync even when connected, for example every few minutes, to defend against:

- dropped connections
- missed events
- auth expiry edge cases
- bugs in event generation

Suggested rule:

- event-driven wake first
- low-frequency fallback resync second

## Server-Side Notes

From Mia's architecture read:

- one idle SSE connection per active agent is acceptable at expected scale
- the expensive part is not the socket count itself but how events are sourced
- v1 should use polling-backed SSE, matching current aweb patterns
- pub/sub can come later if needed

This suggests a pragmatic first server implementation:

- expose a single SSE endpoint
- back it with DB polling
- emit minimal wake events
- add dedicated storage for imperative `control_*` signals

## Legacy and New Platform Implications

This is a shared coordination primitive.

It should not be implemented only in legacy BeadHub because:

- the new aweb platform will need the same agent wake/control pattern
- control semantics belong in the platform layer
- cross-product consistency matters for agent behavior

So the intended layering is:

- aweb OSS: per-agent event stream primitive
- BeadHub legacy: consumes it
- new aweb products: consume it
- `bdh :run`: client implementation for agent wake/control behavior

## What This Replaces in `bdh :run`

The current idle wait/countdown loop should become secondary.

Today:

- sleep
- poll
- sleep
- poll

Target:

- wait on event stream
- fetch snapshot on wake
- only use timed resync as fallback

The visible UI can still show "waiting for events" or similar, but the system should no longer wake on a fixed cadence when idle.

## Open Questions

These are real design questions, but they should not block the first version:

1. Should `work_available` exist in v1, or should chat/mail/control be enough initially?
2. Should `control_interrupt` target one agent, one human's agents, one workspace, or all agents in a project?
3. Do we want event replay via `Last-Event-ID` in v1, or rely on reconnect + snapshot fetch?
4. Should there be explicit event priorities, or are typed events sufficient?

## Recommendation

Build this in two steps:

1. Server primitive
   - add `/v1/events/stream`
   - support `chat_message`, `mail_message`, and `control_*`
   - implement with polling-backed SSE

2. Client adoption in `bdh :run`
   - replace idle polling with stream wait
   - keep snapshot dispatch after wake
   - add interrupt semantics for explicit control events
   - retain low-frequency fallback resync

This is foundational enough that it should be treated as platform work, not just a `bdh :run` optimization.
