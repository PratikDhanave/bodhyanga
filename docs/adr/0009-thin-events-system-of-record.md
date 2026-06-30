# ADR 0009: Thin events + Postgres system-of-record, not fat CaseState messages

**Status:** Proposed

**Date:** 2026-06-30

**Deciders:** project owner (Pratik Dhanave)

> Authored from the principal-engineer architecture review (2026). Gated by
> ADR-0006; tightly coupled to ADR-0008 (substrate) and the durable-bus work.

---

## Context

Every message on the bus today carries the **entire `medical.CaseState` by
value**, JSON-encoded: agents call `medical.EncodeState(state)` to put the full
case (presentation, history, ordered tests, simulated results, hypotheses,
working diagnosis, costs) into `msg.Content`, and `medical.DecodeState` to read it
back (see the supervisor and the orchestrator hops in `agents/supervisor.go` and
`pkg/orchestration/orchestrator.go`). This is the "fat message" pattern: the
message *is* the mutable clinical state.

This is benign under the in-memory monolith with a single ordered handler. It
becomes a **correctness hazard the moment the bus is durable and at-least-once**,
which is exactly what the PR-0.7 NATS rewrite introduces (ack-after-handle +
redelivery; see ADR-0008). Two failure modes appear:

1. **Lost updates / stale overwrites.** Under at-least-once redelivery and
   HPA-scaled concurrent consumers, a redelivered fat message carries a *snapshot*
   of `CaseState` from when it was first published. If newer state has since been
   produced, processing the redelivered (stale) snapshot can overwrite or branch
   from outdated clinical data. There is no version check anywhere to catch this.

2. **Ambiguous system of record.** The authoritative copy of a case is currently
   unclear — is it the in-flight message, the in-memory `medical.PatientRegistry`,
   or a Postgres row (the adapters from PR-0.5)? For a clinical system, the system
   of record must be exactly one place, immutable for the audit-relevant parts.

The Phase-0 work already built the foundation for the fix: `PatientStore`,
`TaskStore`, `ReviewStore` interfaces (`pkg/medical/store.go`, `pkg/tasks/store.go`,
`pkg/hitl/store.go`) with Postgres adapters. What has not followed is the *message
model* — messages still carry fat state instead of thin references.

## Decision drivers

- At-least-once redelivery (ADR-0008) makes fat-state messages a lost-update
  hazard on patient data; "drops a case" (today's at-most-once bug) would be traded
  for "corrupts a case," which is worse.
- A clinical system needs an unambiguous, single system of record.
- The Store interfaces already exist, so the data-access layer for thin events is
  largely in place.
- Message size and schema-versioning across distributed agents (ADR-0007) both
  improve when messages are thin.

## Options considered

### A. Thin events + Postgres as system-of-record with optimistic concurrency (recommended)

- **Pros:** Messages become `{case_id, event_type, minimal_payload}`; agents
  read/write `CaseState` through the Store interfaces against Postgres with a
  `version` column (optimistic concurrency) so a stale write is detected and
  rejected rather than silently applied. Eliminates the lost-update class. Makes
  the system of record unambiguous. Shrinks messages and decouples message schema
  from the full clinical model.
- **Cons:** Real work: define event types, add `version` columns + compare-and-set
  to the adapters, and refactor agents to read-modify-write through the store
  instead of decoding fat state. Adds a DB round-trip per hop (mitigated by the
  fact that clinical hops are not high-frequency).

### B. Keep fat messages but add a version/sequence guard

- **Pros:** Smaller change; detect stale snapshots by carrying a `version` in the
  message and rejecting out-of-order applies.
- **Cons:** Still ships the whole clinical record on every hop (size, PHI-on-wire,
  schema-versioning pain). Does not resolve the system-of-record ambiguity. A
  half-measure that keeps the worst property (mutable state on the wire).

### C. Do nothing; rely on per-case serialization (`MaxAckPending=1` partitioned by case_id)

- **Pros:** No data-model change; serial processing per case avoids concurrent
  stale writes within one consumer.
- **Cons:** Forfeits horizontal scaling per case; does not survive redelivery
  across consumer restarts cleanly; leaves the system of record ambiguous. Brittle.

## Decision

**Proposed: Option A — thin events + Postgres system-of-record with optimistic
concurrency — and it must land BEFORE the durable bus (ADR-0008) is relied upon
for clinical traffic.** Sequencing matters: turning on at-least-once delivery while
messages still carry fat mutable state is the specific combination that risks
silent case corruption. The Store interfaces from Phase-0 are the seam to build on.

## Consequences

### Positive

- Eliminates the lost-update / stale-overwrite class introduced by at-least-once delivery.
- Establishes a single, unambiguous system of record (Postgres) for clinical state.
- Smaller messages; less PHI on the wire; easier schema versioning across agents.

### Negative

- Non-trivial refactor: event taxonomy, `version` columns + compare-and-set,
  agent read-modify-write through the stores.
- A DB round-trip per hop (acceptable at clinical-pipeline frequencies).

### Risks

- If done incompletely (some agents thin, some fat), the inconsistency window
  persists; the migration must be coordinated.
- Optimistic-concurrency conflicts need a defined resolution policy (retry, or
  surface to HITL) so a contended case does not spin.

## References

- `pkg/medical` — `CaseState`, `EncodeState`/`DecodeState` (the fat-message payload)
- `agents/supervisor.go`, `pkg/orchestration/orchestrator.go` — the fat-state hops
- `pkg/medical/store.go`, `pkg/tasks/store.go`, `pkg/hitl/store.go` — the Store seams
- `pkg/persistence/postgres/` — adapters that would gain `version` columns
- ADR-0008 (substrate / durable bus — the trigger), ADR-0007 (topology)
