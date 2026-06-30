# ADR 0008: Harden the safety substrate before distributing

**Status:** Needs-Decision

**Date:** 2026-06-30

**Deciders:** project owner (Pratik Dhanave)

> Authored from the principal-engineer architecture review (2026). Claims are
> grounded in the codebase with `file:line` references that can be checked
> directly. See docs/adr/README.md for how this set relates to ADRs 0001–0005.

---

## Context

Bodh's defensible value is not the LLMs (commodity, swapped by config) but the **safety substrate**: audit/provenance, governance, HITL accountability, eval, explainability. The "agent decoupling" line of work (commits 115922e gateway, f0ba55e/b7a5d77 graduation, 69a3f18 shadow) is pushing toward physically distributed agents. This ADR asks: is the substrate strong enough to survive distribution, and if not, what must be hardened first.

What the code actually shows today:

1. **Audit is fail-open at every seam.** `pkg/orchestration/orchestrator.go:379-389` (`recordAudit`) nil-checks the recorder and, on a `Record` error, only calls `env.Logf` and returns — the clinical message proceeds regardless (also `recordAgentError` at :400-413). The interface doc itself codifies this: `pkg/audit/recorder.go:170-186` says callers "are expected to log-and-continue on failure." The new per-agent gateway repeats the exact pattern: `pkg/gateway/pipeline.go:116-124` (`record`) swallows sink errors with the same rationale. This is a deliberate, documented choice — but it means a compliance log that HIPAA §164.312(b) requires to be complete can silently have holes whenever the sink is unavailable.

2. **The "system of record" is ambiguous — two divergent audit paths exist and the strong one is not wired.** A genuinely tamper-evident recorder exists: `pkg/audit/immutable.go` (`ImmutableRecorder`) computes `Hash = SHA-256(prevHash + "\n" + json(Event))` per line (`Record` :138-199), exposes `VerifyChain` (:205-254), and does fire-and-forget S3 WORM export. But it is wired **nowhere in cmd/care** — only into a standalone `cmd/audit-verify/main.go` and tests. The path actually selected for production is `pkg/persistence/postgres/store.go:502-533` (`RecordAuditEvent`), a plain `INSERT INTO audit_events` whose tamper-resistance is RLS + `GRANT SELECT,INSERT` only (`db/migrations/0001_init.sql:94`, `0005_rls.sql`). That table has **no hash/prev_hash/seq column**; ordering is by wall-clock `at DESC` (`audit_events_tenant_at_idx`). `cmd/care/main.go:661-671` (`selectBackends`) returns either the Postgres adapter or `audit.NewInMemory()` — never the chain-hash recorder. So the cryptographically verifiable artifact and the production artifact are different objects, and the verifiable one is dormant.

3. **No shared ordering across recorders.** Every recorder stamps its own UUIDv4 + `time.Now().UTC()` independently (`pkg/audit/recorder.go:482-490` `stamp`). Only `ImmutableRecorder` carries a monotonic `Seq`. In a distributed world with one recorder per agent, "complete + ordered" degrades to "best-effort, clock-skewed, per-node."

4. **The distributed bus is not yet operational.** `pkg/comm/nats.go` HAS been rewritten since the original review — it now does JetStream at-least-once (Ack only after `safeHandle` succeeds, :294-306), DLQ + Term on poison/exhausted messages, and it compiles + vets cleanly under `-tags nats`. But the integration test (`pkg/comm/nats_integration_test.go`, testcontainers-based) **hung and FAILED after a 600s timeout** in this environment with Docker available — deadlocked in the consume loop at `nats.go:271` (`for msg := range msgs.Messages()`). It also gives no global ordering: `Fetch(10, …)` across independent per-agent durable consumers. So the durable bus is coded but not demonstrably reliable, while the shipped default is `InMemoryBus` (`pkg/comm/bus.go:72-106`), explicitly "no FIFO, no backpressure, no retries, no persistence — a process crash loses them."

5. **Autonomy is being fenced, which raises the stakes.** `pkg/graduation/` (Wilson-score promotion gate + `fence_test.go`, b7a5d77) is actively moving agents toward autonomous operation. Distributing autonomous agents on top of a fail-open, ambiguous-system-of-record substrate is the specific combination this ADR flags as premature.

## Decision drivers

- Regulatory completeness: HIPAA §164.312(b) and 21 CFR Part 11 §11.10(e) require a complete, ordered, tamper-evident audit trail — fail-open holes and a non-chained production table do not satisfy this
- System-of-record clarity: an investigator under breach-scope analysis must be able to point at ONE authoritative, verifiable log, not choose between a dormant hash-chain file and a plain Postgres table
- Distribution amplifies every substrate weakness: fragmenting one fail-open recorder into one-per-agent multiplies silent-gap surface and destroys global ordering
- Don't distribute on an unproven transport: the NATS path compiles but its integration test hangs/fails, and the shipped default bus has no persistence — clinical messages can be silently lost
- Patient-safety asymmetry: a missing audit row or a lost message in a clinical decision pipeline is a categorically worse failure than added latency or operational coupling
- Preserve work already done: the hash-chain recorder, S3 WORM export, and the new gateway pipeline are real assets — the decision should wire and consolidate them, not rebuild
- Velocity cost: hardening the substrate first delays the decoupling roadmap that several recent commits are advancing

## Options considered

### A. Harden-first: make the substrate centralized, tamper-evident, and durable BEFORE any physical distribution (the review's recommendation)

- **Pros:** Directly closes the regulatory gaps: wire ImmutableRecorder (pkg/audit/immutable.go) as the system-of-record so VerifyChain actually covers production, or add a prev_hash/seq column + verification to the Postgres path. Keeps audit a single centralized collector even in a distributed topology, preserving 'complete + ordered'. Forces the NATS bus to prove durability+idempotency (fix the hanging integration test, add ordering/MaxAckPending semantics) before it carries clinical traffic. Builds on existing assets (immutable.go, gateway/pipeline.go) rather than rebuilding. Lowest patient-safety risk; strongest defensibility story since the substrate IS the moat.
- **Cons:** Slows the decoupling roadmap that 115922e/f0ba55e/b7a5d77 are advancing. Requires resolving the hard distributed-systems question of how a 'centralized tamper-evident collector' coexists with independently-deployed agents (a shared audit service becomes a write dependency on the clinical hot path — in tension with today's fail-open posture). Risk of gold-plating the substrate while product/clinical validation waits.

### B. Decide audit semantics first (fail-open vs fail-closed) and unify the system of record, but keep distribution moving in parallel

- **Pros:** Tackles the highest-leverage ambiguity — 'is audit allowed to have holes?' — independently of transport. Wiring ImmutableRecorder or adding hash-chaining to RecordAuditEvent is a contained change to a few files (orchestrator.recordAudit, gateway.record, postgres store, selectBackends). Lets the team keep validating the agent decoupling architecture while the audit story solidifies. A clear written answer (e.g. 'audit is fail-open for availability, but every gap emits a paging metric and is itself an audited event') removes the worst footgun cheaply.
- **Cons:** Leaves the bus question open: agents could be distributed onto InMemoryBus-shaped semantics or an unproven NATS path while audit gets fixed, so a message lost in transit still never reaches audit at all. Parallelism risks the substrate fix always being the lower priority and slipping. 'Unify system of record' across both the file-chain and Postgres paths is itself non-trivial design work.

### C. Distribute now, harden the substrate later (continue current trajectory)

- **Pros:** Maximizes near-term velocity on the decoupling roadmap already in flight (gateway, graduation, shadow). Proves the distributed agent architecture end-to-end sooner, surfacing integration problems early. The gateway pipeline and at-least-once NATS code already exist, so the marginal step feels small.
- **Cons:** Ships the exact anti-pattern the review names: a compliance log fragmented across N fail-open recorders with no shared ordering and no live tamper-evidence (the chain-hash recorder stays dormant). Rides a bus whose integration test currently hangs/fails and whose default has no persistence — clinical messages can be silently dropped with no audit record of the loss. Combined with the autonomy fence (graduation), this distributes autonomous clinical agents on an unverifiable substrate — the highest-severity regulatory and patient-safety exposure, and it directly undermines the platform's core defensibility.

## Decision

This is a Needs-Decision ADR; the owner (platform/clinical-safety lead) should choose, but the evidence points strongly toward Option A, with Option B as the minimum acceptable fallback if velocity pressure is non-negotiable. The framing for the owner:

The central question is not "distributed vs monolith" — it is "do we distribute on top of a substrate that can silently lose audit rows and clinical messages?" The code says yes today: `orchestrator.recordAudit` and `gateway.record` both swallow sink errors, the production Postgres audit table has no cryptographic chain, the verifiable `ImmutableRecorder` is wired only into a CLI and tests, and the NATS integration test hangs and fails. Distribution multiplies each of these.

Three decisions the owner should make explicitly, in order:

1. **Audit completeness semantics.** Keep fail-open (availability over completeness) but make every swallowed `Record` error a first-class, paged, and itself-audited event — OR move to fail-closed for clinical message types. Pick one and write it down; the current state is fail-open-by-default with no alerting, which is the worst of both.

2. **One system of record.** Either promote `pkg/audit/immutable.go` (hash-chain + WORM) to the path `cmd/care/main.go:selectBackends` actually wires, or add `prev_hash`/`seq` columns + a verification routine to the Postgres `audit_events` table. Today there are two divergent paths and the verifiable one is dormant — an investigator cannot be handed a single authoritative log. **Note this is a drift from an _Accepted_ decision:** ADR-0002 ("Chain-Hash Audit Trail", Accepted 2024-12-20) specifies `ImmutableRecorder` as the production chain-hash sink, but the production wiring never adopted it. Resolving this means either re-affirming ADR-0002 (wire `ImmutableRecorder`) or superseding it with the Postgres-chain approach — until then, an accepted HIPAA-compliance control is not in force.

3. **Distribution gate.** Do not let clinical traffic onto the NATS bus until `pkg/comm/nats_integration_test.go` passes reliably (it currently times out at 600s) and ordering/idempotency are demonstrated, and keep audit a centralized collector even after agents are split out, so "complete + ordered" survives.

If all three are addressed, distribution is safe to proceed (Option A). If only #1 and #2 are addressed, distribution may proceed on the bus only after #3 (Option B). Continuing to distribute without any of them (Option C) is not recommended — it trades the platform's core defensible asset for roadmap speed.

## Consequences

### Positive

- A single, verifiable system of record: wiring ImmutableRecorder (VerifyChain, pkg/audit/immutable.go:205) or chaining the Postgres path gives an investigator one authoritative, tamper-evident artifact for HIPAA §164.312(b) / 21 CFR Part 11 breach-scope analysis
- Silent audit gaps become visible: turning swallowed Record errors (orchestrator.go:387, pipeline.go:121) into paged, self-audited events closes the fail-open hole without necessarily sacrificing availability
- Distribution proceeds on proven rails: a green nats_integration_test.go and demonstrated ordering/idempotency mean clinical messages are not silently lost when agents are split out
- The platform's defensible moat (the safety substrate) is strengthened exactly where the review says it is weakest, rather than diluted by distribution
- Existing assets are reused, not rebuilt: immutable.go, S3 WORM export, and gateway/pipeline.go all become load-bearing

### Negative

- The agent-decoupling roadmap (commits 115922e, f0ba55e, b7a5d77, 69a3f18) slows while the substrate is hardened
- A centralized tamper-evident audit collector becomes a write dependency for distributed agents, in tension with today's fail-open posture — resolving that tension (async durable queue to the collector? local chain segments stitched centrally?) is real design work
- Hash-chaining or a synchronous central collector adds latency/throughput cost to the clinical hot path that the current per-event, non-fsync'd, fail-open design deliberately avoids (recorder.go:288-314 notes the throughput trade-off)
- Adding prev_hash/seq to audit_events is a schema migration with backfill and reader-compatibility concerns during rolling deploys

### Risks

- Gold-plating: over-investing in cryptographic audit while clinical/product validation waits, if the owner picks A without scoping it tightly
- Half-measure trap: choosing B and never funding the bus-durability work (#3), leaving messages lossy in transit so they never reach even a perfect audit log
- Distributed-ordering gap persists: per-agent recorders each stamp independent UUID+wall-clock timestamps (recorder.go:482), so without a shared sequence the 'ordered' guarantee remains clock-skew-dependent even after hardening
- NATS reliability is deeper than the test timeout suggests: the 600s hang at nats.go:271 may indicate a consume-loop or fetch-deadline defect that is non-trivial to fix, not just a flaky container
- Proceeding with Option C in the interim (while 'deciding') quietly ships the anti-pattern, because the autonomy fence (pkg/graduation) keeps advancing agent independence regardless of the audit decision

## References

- pkg/orchestration/orchestrator.go:379
- pkg/orchestration/orchestrator.go:400
- pkg/audit/recorder.go:170
- pkg/audit/recorder.go:482
- pkg/audit/immutable.go:138
- pkg/audit/immutable.go:205
- pkg/gateway/pipeline.go:116
- pkg/persistence/postgres/store.go:502
- cmd/care/main.go:661
- pkg/comm/bus.go:72
- pkg/comm/nats.go:294
- pkg/comm/nats_integration_test.go
- db/migrations/0001_init.sql:94
- pkg/graduation/fence_test.go
