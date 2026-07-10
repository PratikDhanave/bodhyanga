# Bodh — Architecture

> **Audience.** Engineers extending Bodh, reviewers auditing its claims, and operators
> deciding what it is safe to run. This document is written to be *falsifiable*: every
> non-obvious claim carries a `file:line` anchor you can open and check, and every gap
> between the documented architecture and the running binary is stated here rather than
> left for you to discover.
>
> **Companion docs.** [agents.md](agents.md) (per-agent behaviour), [governance.md](governance.md)
> (policy contracts), [data-model.md](data-model.md) (clinical types), [operations.md](operations.md)
> (audit/observability ops), [deployment.md](deployment.md) (topology), and the decision
> records in [docs/adr/](adr/). Where this document and an ADR disagree, this document
> reflects the **current code**; the divergence is called out inline with the commit that
> caused it.

---

## 0. How to read this document

The single most important thing to understand about Bodh is **what it actually is**, so
Section 1 settles that before anything else. Sections 2–3 give the system context and the
invariants the whole design defends. Sections 4–10 are the substrate and clinical flows as
they run today (the monolith). Section 11 is the in-progress decoupling. Sections 12–15 are
the failure model, threat model, and the honest gap register. Section 16 is the decision log.

Reading order for a **reviewer trying to break the claims**: Section 1 (identity), then
Section 3 (invariants — each has its enforcement site *and* its known hole), then Section 15
(the gap register). Everything the marketing might oversell is disclosed in those three
sections with citations.

---

## 1. Identity and scope — what Bodh *is*

Bodh is a **research and education reference architecture** for multi-agent medical AI
orchestration, written in Go. It is **not a medical device, not certified, not under a
Business Associate Agreement, and not for clinical use** (`NOTICE`). The phrase
"HIPAA-aware" in the README denotes *architectural intent* — the substrate is modelled on
real regulatory text (HIPAA §164.312(b) audit controls, 21 CFR Part 11, 42 CFR Part 2,
SMART-on-FHIR) — **not** certified compliance.

This distinction is not a disclaimer bolted on for safety; it is the **central architectural
fact**, formalized in [ADR-0006 (Project Identity)](adr/0006-project-identity.md), status
*Needs-Decision*. ADR-0006's own words: Bodh "currently presents itself as **two different
products at once**, and the codebase honestly reflects both — which is the root cause of much
of the architectural incoherence the other ADRs address."

### 1.1 What actually runs vs. what is designed

The default `cmd/care` binary boots a **research-grade substrate**. The gap between the
running service and the "clinical-grade" design is real, bounded, and enumerated here:

| Capability | Designed / built | Wired in default `cmd/care`? | Evidence |
|---|---|---|---|
| Governance policy stack | 12+ policies exist | **5 wired** (`MaxContentLength`, `RequireCaseID`, `MaxDiagnosticCost`, `ResearchDisclaimer`, `AgentMeshGate`) | `cmd/care/main.go:71-77` |
| Tamper-evident audit (chain-hash) | `ImmutableRecorder`, full SHA-256 chain + S3 WORM export (`pkg/audit/immutable.go:64`) | **No** — wired only in `cmd/audit-verify` + tests | `cmd/care/main.go:661-671` selects Postgres-plain-INSERT or in-memory |
| Durable message bus | NATS JetStream, at-least-once + DLQ (`-tags nats`) | **No** — default is `InMemoryBus` (no persistence, no FIFO) | `pkg/comm/bus.go:85-106` |
| Authenticated API | `pkg/auth` (OPA Rego + JWT), `SMARTScopePolicy` | **No auth middleware wired** | ADR-0006; `cmd/care` has no auth on clinical routes |
| Encryption at rest | AES-256-GCM full-record seal | **Only if `BODH_ENCRYPTION_KEY` is set** — unset ⇒ plaintext passthrough | `pkg/persistence/crypto.go:54-71` |
| Persistence backend | Postgres + RLS + append-only grants | **In-memory by default**; Postgres opt-in via `BODH_DB_DSN` | `cmd/care/main.go:720-729` |
| Autonomy ladder / eval gate | `pkg/graduation` (Wilson gate + autopilot fence) | **No production caller** — library only | grep: no `graduation.Evaluate` outside package |

None of these are accidents. They are the substrate-first posture of
[ADR-0008](adr/0008-substrate-first.md): build the interfaces and the strong
implementations, but do not claim the running service enforces them until the wiring and the
operational proof exist. This document treats the "designed" column as roadmap and the
"wired" column as truth.

### 1.2 Scope boundaries

**In scope:** the orchestration substrate (bus, registry, governance, audit), the clinical
agent set (diagnosis + care-services), the LLM abstraction and its fallback contract, PHI
de-identification, and the agent-decoupling substrate (Phase 0/1). **Out of scope of the
running binary:** authentication/authorization enforcement, certified tamper-evident audit,
durable delivery guarantees, autonomous (human-out-of-the-loop) clinical action. Those are
documented as roadmap in Section 15, each tied to the ADR that owns the decision.

---

## 2. System context and trust boundaries

```
                        ┌─────────────────────────────────────────────────────┐
                        │  cmd/care process (default: single OS process)       │
   clinician / API      │                                                      │
   client  ───TB1──────▶│  HTTP API (pkg/web)   embedded React SPA             │
   (unauthenticated     │        │                                             │
    by default)         │        ▼                                             │
                        │   protocol.Message ──TB2──▶ Orchestrator ────┐       │
                        │                             (governance      │       │
                        │                              choke point)    │       │
                        │                                   │          ▼       │
                        │                                   │      InMemoryBus  │
                        │        ┌──────────────────────────┘      (TB3)       │
                        │        ▼                                   │         │
                        │   19 agents (rule + LLM)  ◀────────────────┘         │
                        │        │            │                                │
                        │        │            └──TB5──▶ LLM provider (external) │
                        │        ▼                      Anthropic/OpenAI/Ollama │
                        │   audit.Recorder (TB4)                               │
                        │   medical/tasks/hitl stores (TB6)                    │
                        └─────────────────────────────────────────────────────┘
                                 │
                                 ▼ (opt-in: BODH_DB_DSN)
                          Postgres (RLS, append-only audit, AES-GCM at rest)
```

**Trust boundaries** (analysed in Section 14):

- **TB1 — API ingress.** Between an external client and the service. *No authentication is
  wired by default* (ADR-0006). Identity-bearing metadata (`tenant_id`, `scopes`) is
  therefore client-asserted, not verified.
- **TB2 — Governance choke point.** Every message crosses `policy.Evaluate` before any agent
  sees it. This is the *only* place governance runs (monolith).
- **TB3 — Bus.** In-process and trusted by default; becomes a *network* boundary under NATS.
- **TB4 — Audit sink.** Fail-open; the production sink is a mutable Postgres table (no
  chain-hash), not the tamper-evident recorder.
- **TB5 — LLM egress.** Message content leaves the process to a third-party model unless
  de-identified first.
- **TB6 — Persistence.** Tenant isolation via Postgres RLS; encryption is
  configuration-dependent.

---

## 3. Architectural invariants

These are the properties the design defends. Each is stated as a falsifiable claim, with its
enforcement site **and** its known limitation. An invariant with a hole is still documented
as an invariant — the hole is the honest part.

**I1 — Governance is fail-closed.** A message that a policy denies, or that a policy errors
on, never reaches an agent. *Enforced:* `pkg/orchestration/orchestrator.go:255-287` (monolith)
and `pkg/gateway/pipeline.go:57-77` (decoupled): a policy error is treated as deny (`return
false`), and a `DecisionDeny` short-circuits before `HandleMessage`. `CompositePolicy` denies
on the first child deny (`pkg/governance/policy.go:164`). *Known hole:* a policy **error**
(as opposed to a deny) is logged and dropped **without an audit event** in the monolith
(`orchestrator.go:260-267`) — a deliberate, documented gap. Gateway matches this behaviour
(`pipeline.go:62-65`).

**I2 — Audit never blocks clinical flow (fail-open).** If the audit sink errors, the message
still processes. *Enforced:* `orchestrator.go:364-389` (`recordAudit` nil-checks the recorder
and only `Logf`s on `Record` error); gateway repeats the pattern at `pipeline.go:116-124`.
*Consequence, not bug:* the compliance log HIPAA §164.312(b) requires to be *complete* can
silently have holes when the sink is down. ADR-0008 flags this as an open decision.

**I3 — PHI never enters an audit event.** *Structurally enforced:* the `audit.Event` struct
has **no `Content` field** (`pkg/audit/recorder.go:91-164`); `FromMessage`/`FromPolicyDecision`
copy only routing fields, and `pickStrings` whitelists exactly four metadata keys —
`patient_id`, `case_id`, `trace_id`, `tenant_id` (`recorder.go:455-475`). The audit package
has no code path that can read `msg.Content`. `KindAgentError` carries `err.Error()` only,
never content (`orchestrator.go:400-413`).

**I4 — PHI never enters raw telemetry.** Case identifiers on spans are `sha256(caseID)[:8]`
hex; raw MRNs never appear in collectors or logs (ADR-0005, enforced at span creation in
`pkg/observability` and by `phi.LoggerWrapper` in the log pipeline). *Known limit:* regex
redaction catches structured identifiers (SSN, MRN, phone, email, dates) but **cannot** catch
free-form names/addresses in narrative — acknowledged in the package docs (ADR-0005).

**I5 — The orchestrator contains zero clinical logic.** It routes by `msg.To` and never
branches on diagnosis, condition, or test results. *Enforced by construction:*
`orchestrator.go:221-362` is pure transport + policy + audit; adding an agent or condition is
zero orchestrator-side change. The `DiagnosticSupervisor` state machine is likewise pure
routing on `msg.Type` (`agents/supervisor.go:74-114`).

**I6 — Every LLM decision has a deterministic fallback, so every case finalizes.** *Enforced:*
`LLMDiagnostician.Fallback` is a required field (`agents/diagnostician_llm.go:28-30`); any
LLM failure — timeout, malformed JSON, empty diagnosis, confidence `< MinConfidence` —
routes to `runFallback` → the rule engine, which always emits a `DiagnosisProposal`
(`diagnostician_llm.go:203-234`, `diagnostician.go:62-100`). Debate, Reflexion, and Cascade
all chain back to the same fallback.

**I7 — Autopilot (autonomous clinical action) is a fenced, authorization-gated change.** The
graduation gate checks `Criteria.AllowAutopilot` (default `false`) **before** the statistical
gates, so a denied promotion audits as an authorization decision, not a statistical near-miss
(`pkg/graduation/graduation.go:90-94`; verified `fence_test.go:11-22` — a perfect 1000/1000
record still holds at copilot). *Known hole:* `pkg/graduation` has **no production caller**
(ADR-0012) — the fence protects a mechanism nothing currently invokes.

---

## 4. The MARA substrate

Bodh implements Microsoft's Multi-Agent Reference Architecture (MARA); each concept maps to a
Go package (ADR-0004). The tables below are corrected against the code.

| MARA concept | Package | Key type | Notes |
|---|---|---|---|
| Message | `pkg/protocol` | `Message` | `agent.Message` is a type alias (`pkg/agent/types.go:44`) |
| Agent contract | `pkg/agent` | `Agent`, `Environment` | 5-method + 2-method interfaces |
| Registry | `pkg/registry` | `Registry`, `InMemoryRegistry` | **no** hot-reload (§4.5) |
| Bus | `pkg/comm` | `Bus`, `InMemoryBus`, `NATSBus` (`-tags nats`) | |
| Orchestrator | `pkg/orchestration` | `Orchestrator`, `SimpleEnvironment` | the governance choke point |
| Governance | `pkg/governance` | `Policy`, `CompositePolicy` | §5 |
| Audit | `pkg/audit` | `Event`, `Recorder` | §6 |
| PHI redaction | `pkg/phi` | `Redactor`, `LoggerWrapper` | §8 |
| Evaluation | `pkg/eval` | `InteractionRecord`, `CaseMetrics`, `Judge` | §10 |
| HITL gate | `pkg/hitl` | `Queue`, `ReviewStore`, `BuildResumeMessage` | §9.4 |

### 4.1 The five core interfaces

**`Agent`** (`pkg/agent/types.go:114-154`):

```go
type Agent interface {
    ID() string                 // bus address; used as Message.To
    Name() string               // human label
    Capabilities() []string     // queryable via Registry.FindByCapability
    HandleMessage(ctx context.Context, msg Message, env Environment) ([]Message, error)
}
```

`HandleMessage` returns zero or more messages; the orchestrator republishes them. `nil, nil`
means "handled, no follow-up" (terminal agents); a non-nil error causes a `KindAgentError`
audit event and drops any partial outputs.

**`Bus`** (`pkg/comm/bus.go:55-70`):

```go
type Bus interface {
    Subscribe(agentID string, h Handler) (unsubscribe func())
    Publish(ctx context.Context, msg protocol.Message)
}
type Handler func(ctx context.Context, msg protocol.Message)
```

Routing is by exact string match on `msg.To`, plus broadcast subscribers registered under
`""` (observability sinks only). Agents never call `Publish` directly — only the orchestrator
does, after governance and audit.

**`Registry`** (`pkg/registry/registry.go:50-82`): `Register`, `Get`, `List`,
`FindByCapability`. `Register` **replaces** on duplicate ID (no error, intentional). There is
**no** `Unregister`, `Refresh`, or `Watch` (see §4.5).

**`Policy`** (`pkg/governance/policy.go:94-96`):

```go
type Policy interface {
    Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error)
}
type PolicyResult struct {
    Decision    Decision   // DecisionAllow | DecisionDeny  (policy.go:23-38)
    Reason      string
    CheckedAt   time.Time
    CheckedByID string     // reserved for per-policy attribution; currently unset
}
```

**`Environment`** (`pkg/agent/types.go:183-199`): deliberately two methods — `Now()` and
`Logf(...)`. Extend by composition (add fields to a concrete type, type-assert in the agent),
never by widening this interface. The shipped `SimpleEnvironment` carries a `Logger` and a
`Clock`; production swaps `Logger` for `phi.LoggerWrapper` and `Clock` for a fake in tests.

### 4.2 Message protocol

```go
type Message struct {          // pkg/protocol/message.go:71-97
    ID        string            // UUID
    From      string            // sender agent id or "user"
    To        string            // recipient agent id (routing address); "" ⇒ broadcast
    Role      MessageRole       // user|system|agent|tool|observer|evaluator
    Type      string            // handler branch key
    Content   string            // primary payload (often JSON CaseState) — PHI lives here
    CreatedAt time.Time         // UTC
    Metadata  map[string]any    // correlators + control envelope (see below)
}
```

`Content` is the PHI-bearing field; it stays on the wire and is **never** copied into audit
(I3) or raw telemetry (I4). Metadata is an open map; the audit whitelist (I3) is what keeps it
from becoming a PHI leak. Key metadata conventions: `case_id` (all clinical types),
`patient_id` (care-services, opaque token — never an MRN), `tenant_id` (RLS key), `trace_id`
(OTel), cost keys (`max_cost_usd`, `test_cost_usd`, `spent_usd`), and the HITL envelope
(`hitl_kind`, `hitl_next_to`, `hitl_next_type`, `hitl_priority`, `hitl_subject`,
`hitl_sla_minutes` — §9.4).

### 4.3 The orchestrator closure — the only place governance runs

`Orchestrator.Start` (`orchestrator.go:221-362`) snapshots the registry once and subscribes
each agent with a closure that runs five ordered steps per message. This is the exact shape —
reproduced faithfully, not paraphrased:

```go
// pkg/orchestration/orchestrator.go:244-359 (elided for length; order & semantics exact)
o.bus.Subscribe(agentID, func(c context.Context, msg agent.Message) {
    // Step 1 — Governance (fail-closed).
    if o.policy != nil {
        res, err := o.policy.Evaluate(c, msg)
        if err != nil {
            o.env.Logf("policy error for msg %s: %v", msg.ID, err)
            return                                   // dropped; NO audit event (known gap, I1)
        }
        if res.Decision == governance.DecisionDeny {
            o.env.Logf("message %s denied by policy: %s", msg.ID, res.Reason)
            o.recordAudit(audit.FromPolicyDecision(msg, res))  // deny always audited
            return                                             // agent never sees it
        }
        if clinicalMessageTypes[msg.Type] {          // allow: audit clinical types only
            o.recordAudit(audit.FromPolicyDecision(msg, res))
        }
    }
    // Step 2 — Record the invocation BEFORE running (a panicking handler still leaves a trail).
    o.env.Logf("agent %s handling message %s from %s", agentID, msg.ID, msg.From)
    o.recordAudit(audit.FromMessage(agentID, msg))

    // Step 3 — Delegate.
    out, err := handler.HandleMessage(c, msg, o.env)
    if err != nil {
        o.env.Logf("agent %s error: %v", agentID, err)
        o.recordAgentError(agentID, msg, err)        // KindAgentError, err text only (I3)
        return
    }
    // Step 4 — Eval mirror (side-channel): terminal supervisor diagnosis → eval.Store.
    if len(out) == 0 && agentID == "diagnostic_supervisor" && msg.Type == "diagnosis" {
        o.recordCaseEval(msg)
    }
    // Step 5 — Fan-out. One agent's output is another's input.
    for _, m := range out {
        o.bus.Publish(c, m)
    }
})
```

Note the two audit subtleties a reviewer should verify: (a) the **policy-error path records
nothing** (`orchestrator.go:260-267`), and (b) **allow events are recorded only for the six
clinical types** in `clinicalMessageTypes` (`orchestrator.go:31-38`) to keep the log
signal-rich; non-clinical allows are intentionally suppressed. That set is **duplicated** in
`RequireCaseIDPolicy` (`pkg/governance/medical_policy.go:47-50`) — a deliberate copy to avoid
an import cycle, documented at `orchestrator.go:24-27`. Add a clinical type and you must edit
both.

`Start` is idempotent via `sync.Once` (`orchestrator.go:221-226`). Audit and eval recorders
are set via `SetAuditRecorder`/`SetEvalStore` **before** `Start`; the closures capture them at
subscribe time, so a post-`Start` swap is silently ignored (`main.go:120-131`).

### 4.4 Bus semantics

**`InMemoryBus` (default).** Each `Publish` invokes matching handlers in **separate
goroutines** via `safeHandle`, which recovers panics so one buggy agent cannot crash the
process (`bus.go:277-294`). Its properties are stated as anti-guarantees in the source
(`bus.go:85-106`): **no FIFO, no backpressure, no retries, no persistence** — a crash loses
in-flight messages. Ordering in the diagnostic flow comes from the supervisor's state
machine, not the bus (§12).

**`NATSBus` (`-tags nats`).** At-least-once delivery with a dead-letter queue. `safeHandle`'s
boolean return drives the Ack/Nak decision: a clean handler run Acks; a panic Naks toward
redelivery/DLQ (`bus.go:272-294`). *Status correction:* ADR-0008 records the NATS integration
test as hanging past a 600s timeout. That deadlock was a missing `ctx.Err()` guard in the
fetch loop and was **fixed** in commit `938ab09` ("honour context cancellation in NATSBus
fetch loop"); the three testcontainer-backed integration tests now pass (~7s). This document
supersedes the ADR-0008 snapshot on that specific point. The durable bus is compiled only
under `-tags nats` and is **not** in the default binary.

### 4.5 Registry semantics (and the hot-reload myth)

`InMemoryRegistry` is an RWMutex-guarded map with a capability index. It supports
`FindByCapability` — used by `inbox_triage` to discover a primary-care agent at runtime rather
than hard-coding a peer. It does **not** support removal or hot reload. Prior documentation
claimed `Orchestrator.Refresh`/`Watch` methods that re-list the registry on an interval —
**those methods do not exist** in either `pkg/registry` or `pkg/orchestration`. Hot reload is
a roadmap comment (`orchestrator.go:63-64`): agents registered after `Start` are not
subscribed. If you see `Refresh`/`Watch` referenced anywhere, it is stale.

### 4.6 Environment and logger composition

Production wiring wraps the inner logger in `phi.LoggerWrapper{Redactor, Inner}` so PHI
patterns are redacted before any third-party sink (I4). Agents call `env.Now()` rather than
`time.Now()` so tests inject a deterministic clock. Every `llm.Client.Complete` produces an
`observability.LLMTrace`; a hook forwards it to OpenTelemetry under `-tags otel`.

---

## 5. Governance stack

### 5.1 The live stack vs. the arsenal

The production stack wired in `cmd/care/main.go:71-77` is **five policies, evaluated in
order** (cheapest/most-discriminating first; `CompositePolicy` short-circuits on first deny,
`policy.go:145-186`):

1. `MaxContentLengthPolicy{Max: 65536}` — rejects oversized payloads (`policy.go:188-236`).
2. `RequireCaseIDPolicy{}` — clinical types must carry a string `case_id`; non-clinical types
   bypass (`medical_policy.go:33-70`).
3. `MaxDiagnosticCostPolicy{}` — denies `order_test` when `spent_usd + test_cost_usd >
   max_cost_usd`; fail-open on missing keys (the `cost_guardian` agent is the defence-in-depth
   backstop) (`medical_policy.go:95-145`).
4. `ResearchDisclaimerPolicy{}` — always allows; stamps "research demo — not for clinical use"
   into the audit reason (`medical_policy.go:162-167`).
5. `AgentMeshGate` — global + per-agent kill switches, ring-based access matrix, and a
   prompt-injection risk scorer that denies at/above `MaxRiskScore` (default 40)
   (`agentmesh_gate.go:114-226`).

`cmd/demo` and `cmd/caredemo` run the same stack **minus** `AgentMeshGate`.

**Built but not wired** (the arsenal, a Section-1 substrate-first fact): `BreakGlassPolicy`
(emergency override + `KindBreakGlass` audit, `break_glass_policy.go`), `Part2ConsentPolicy`
(42 CFR Part 2 SUD segmentation, `part2_policy.go`), `RateLimitPolicy` (**stateful**
token-buckets per tenant+agent, `rate_limit_policy.go:85-187`), `SMARTScopePolicy` (JWT scope
validation, `smart_scope_policy.go`), `OPAPolicy` (Rego, `opa_policy.go`), `RequireTenantPolicy`,
`RequireScopePolicy`, and `RedactNarrativePolicy`. These are real, tested, and composable —
they are simply not in the default chain. That "built ≠ wired" gap is the honest state.

*Correction to prior docs:* the old "six shipped policies" list (`MaxContentLength`,
`RequireCaseID`, `MaxDiagnosticCost`, `ResearchDisclaimer`, `RequireTenant`, `RequireScope`)
was wrong on both ends — `RequireTenant`/`RequireScope` are **not** wired, and `AgentMeshGate`
**is**. And "policies are stateless" is stale: `RateLimitPolicy` holds mutex-guarded state.

### 5.2 Composition semantics

`CompositePolicy.Evaluate` runs children in slice order; the first `DecisionDeny`
short-circuits and returns (`policy.go:164`); a child **error** propagates unchanged and the
orchestrator treats it as a deny (fail-closed, I1). A child that returns a zero `CheckedAt`
has it backfilled with the composite clock (`policy.go:169-170`).

---

## 6. Audit pipeline

### 6.1 Event schema and kinds

`audit.Event` (`recorder.go:91-164`) is exactly **16 fields**, all routing/correlation, no
narrative: `ID`, `At`, `Kind`, `AgentID`, `MessageID`, `From`, `To`, `Type`, `TenantID`,
`PatientID` (opaque token, not MRN), `CaseID`, `TraceID`, `Decision`, `Reason`, `ReviewerID`,
`ErrorText`. There is no `Content` field — the PHI guarantee is structural (I3).

`Kind` has **five** constants (prior docs listed four): `KindMessage`, `KindPolicyDecision`,
`KindReviewDecision`, `KindAgentError` (`recorder.go:50-69`), and `KindBreakGlass`
(`audit_kinds.go:24`, "unconditionally audited regardless of other filters").

The `Recorder` interface is `Record(e Event) error` + `List() []Event` (`recorder.go:166-186`),
with a documented fail-open contract: callers log-and-continue on `Record` error.

Constructors (note the exact argument order — prior docs had these reversed):
`FromMessage(agentID string, msg protocol.Message)` (`recorder.go:382`) and
`FromPolicyDecision(msg protocol.Message, res governance.PolicyResult)` (`recorder.go:409`).
Both extract correlators via `pickStrings` (4-key whitelist, `recorder.go:455-475`).

### 6.2 Recorders — and the system-of-record drift

Three implementations exist:

- **`InMemory`** (`recorder.go:188-233`) — mutex-guarded slice, lost on restart. Tests/demos.
- **`FileStore`** (`recorder.go:235-372`) — append-only JSONL, mode `0600`, no per-event
  fsync (a downstream forwarder provides durability). *Not* WORM.
- **`ImmutableRecorder`** (`immutable.go:64-326`) — the tamper-evident sink ADR-0002 specifies:
  each line is a `ChainedEvent{Event, PrevHash, Hash, Seq}` with
  `Hash = SHA-256(PrevHash + "\n" + json(Event))`, genesis hash all-zeros, monotonic `Seq`,
  constant-time `VerifyChain`, and fire-and-forget gzip export to S3 with Object Lock.

**The drift (the doc's single most important honesty point).** ADR-0002 is *Accepted* and
names `ImmutableRecorder` the production audit path. It is wired **nowhere** in `cmd/care`.
`selectBackends` (`main.go:661-671`) returns either the Postgres adapter or `audit.NewInMemory()`
— **never** the chain-hash recorder. The Postgres path (`store.go:502-533`) is a plain
`INSERT INTO audit_events` whose tamper-resistance is **only** RLS + `GRANT SELECT,INSERT`
(append-only at the grant layer) — the table has **no** `hash`/`prev_hash`/`seq` columns. So
the cryptographically verifiable artifact and the production artifact are different objects,
and the verifiable one is dormant (used only by `cmd/audit-verify` and tests). ADR-0008 calls
this out and requires a decision: re-affirm ADR-0002 (wire `ImmutableRecorder`) or supersede
it (add hash-chain columns + a verifier to Postgres). Until then, **an accepted HIPAA
§164.312(b) control is not in force in the running binary.**

### 6.3 Is audit on by default?

Yes — `cmd/care` calls `SetAuditRecorder` before `Start` (`main.go:130`), defaulting to
`audit.NewInMemory()` unless `BODH_DB_DSN` selects the Postgres recorder. Every message
produces a `KindMessage` event; every deny produces a `KindPolicyDecision`; allows are audited
only for clinical types (§4.3); the policy-error path produces nothing (I1). Combined with
fail-open (I2), "every clinical message produces an audit event" is true **only while the sink
is healthy** — precisely the completeness gap ADR-0008 wants closed.

---

## 7. Persistence and tenancy

`pkg/persistence/postgres` implements five adapters, each with a compile-time interface
assertion: `evalAdapter → eval.Store` (`store.go:277`), `PatientRegistryAdapter →
medical.PatientRegistry` (`store.go:461`; note `medical.PatientStore` is *not* backed by
Postgres), `auditAdapter → audit.Recorder` (`store.go:605`), `TaskStoreAdapter →
tasks.TaskStore` (`task_store.go:104`), and `ReviewStoreAdapter → hitl.ReviewStore`
(`review_store.go:245`).

### 7.1 Row-level security

Every tenant-bearing table has RLS **forced**. Each read/write runs inside `withTenant`
(`store.go:178-200`), which opens a transaction and executes
`SELECT set_config('app.tenant_id', $1, true)` (transaction-scoped `SET LOCAL`) before the
query. The policy (`migrations/0002_rls.sql:29-33`) is
`USING (tenant_id = current_setting('app.tenant_id', true))` with a matching `WITH CHECK`.
The application role `bodh_app` is created `NOBYPASSRLS`, and the migration re-strips any
`BYPASSRLS` on every run (testcontainer images sometimes ship it). A runtime guard,
`CurrentRoleHasBypassRLS()` (`store.go:148-160`), aborts if RLS is not actually enforced.
Integration tests connect first as the owner (to run migrations) then as `bodh_app` (so RLS
applies), isolating tests by synthetic per-test `tenant_id` rather than separate databases
(`pkg/dbtest/dbtest.go:98-151`).

### 7.2 Append-only audit

`audit_events` gets `GRANT SELECT, INSERT` only (`0002_rls.sql:98`) plus separate SELECT/INSERT
RLS policies and **no** UPDATE/DELETE policy — append-only enforced at *two* layers (grant and
policy). `DeletePatient` deliberately does **not** cascade into `audit_events`; the trail keeps
the pseudonymous `patient_id` for compliance investigations (`store.go:432-449`) — correcting
the prior "cascades through audit_events" claim.

### 7.3 Encryption at rest — by configuration, not by default

Reviews, patient records, and nurse tasks are sealed whole with **AES-256-GCM** (12-byte
random nonce ‖ ciphertext ‖ 16-byte tag) into a `payload_ct` column; only non-PHI lifecycle
columns (`status`, `submitted_at`) stay cleartext for queries (`review_store.go:189-212`,
`0005_reviews.sql:4-17`). The key comes from `BODH_ENCRYPTION_KEY` (base64 32 bytes). **If the
variable is unset, the encryptor is a no-op passthrough and PHI is stored as plaintext**
(`crypto.go:54-71`). This is safe for local dev and a latent liability in any real deployment
— it is "secure by configuration," not "secure by default," and operators must set the key.

### 7.4 Backend selection

`BODH_DB_DSN` unset ⇒ in-memory + file backends (`main.go:720`). Set ⇒ Postgres adapters,
RLS-scoped to `BODH_TENANT_ID`. A Postgres open failure at startup **silently falls back to
in-memory** (`main.go:725-729`) — convenient for demos, but a deployment that expected durable
Postgres and mis-set the DSN would run in-memory without a hard failure. Worth an operator
alert in production wiring.

---

## 8. PHI hygiene (three-layer defence, ADR-0005)

1. **Telemetry correlation tokens.** Span/case identifiers are `sha256(caseID)[:8]`. The join
   is one-way: an authorized investigator who *knows* a case ID can compute the token and find
   spans; the token alone does not reveal the case (I4).
2. **Log redaction.** `phi.LoggerWrapper` applies regex patterns (SSN, MRN, US phone, email,
   ISO/US dates) before any sink. Limitation (disclosed): free-form names/addresses in
   narrative are not caught.
3. **Structural audit exclusion.** `msg.Content` is never copied into an `Event` (I3).

---

## 9. Clinical flows

### 9.1 Sequential diagnosis state machine

The `DiagnosticSupervisor` (`agents/supervisor.go`) is a pure router on `msg.Type`:

| Incoming type | Routed to | Next type | Site |
|---|---|---|---|
| `case_state` (from intake/rpm) | `questioner` | `case_state` | supervisor.go:175-179 |
| `questions_complete` | `test_planner` | `case_state` | supervisor.go:188-195 |
| `tests_complete` | `diagnostician` | `case_state` | supervisor.go:196-203 |
| `reasoning_verified` | `diagnostician` | `case_state` | supervisor.go:204-214 |
| `tests_revise` | `cost_guardian` | `case_state` | supervisor.go:135-167 (cost loop) |
| `diagnosis` | — (terminal) | — | supervisor.go:231-259 |

The verified demo trace (`cmd/demo`, 8 agents, no HITL gate, no cost rework) is **12
messages**, `user → intake → diagnostic_supervisor → questioner → … → reasoning_verifier →
diagnostic_supervisor(terminal)`, stamped `message_steps: 12`. Terminal detection is
`len(out)==0 && agentID=="diagnostic_supervisor" && msg.Type=="diagnosis"`
(`orchestrator.go:340`), which triggers the eval mirror. In `cmd/care` the HITL gate on
`reasoning_verifier` adds a review hop, so the live path is longer.

### 9.2 The LLM diagnostician and its patterns

`pkg/llm.Client` is `Provider() / Model() / Complete(ctx, Request) (*Response, error)`
(`client.go:10-23`), implemented by Anthropic, OpenAI, Ollama, and (build-tagged) Kronk
in-process llama.cpp. The diagnostician factory (`agents/diagnostician_factory.go`) selects
one of four patterns, all sharing the I6 fallback:

- **Single** (`diagnostician_llm.go`) — 1 call. JSON-schema + GBNF-grammar-constrained
  `DiagnosisProposal`, optional RAG evidence, confidence floor `MinConfidence` (default 0.3).
- **Debate** (`diagnostician_debate.go`) — N+1 calls. N specialist personas (default 4:
  Cardiology, Infectious Disease, Pulmonology, Emergency Medicine) run in parallel (semaphore
  ≤4); a chair synthesizes. Quorum <50% ⇒ fallback.
- **Reflexion** (`diagnostician_reflexion.go`) — 1 draft + 1‥`MaxRounds` critiques. **Default
  `MaxRounds` is 1, clamped `[1,3]`** (correcting the prior "default 2"). Stops early on
  `Verified=true`.
- **Cascade** (`diagnostician_cascade.go`) — 1‥2 calls. Small tier first; escalate to the
  large tier if confidence `< EscalateAt` (default 0.6).

### 9.3 Care-services agents

Nine care-services agents share the same bus/registry/governance and hold
dependency-injected state (wired in `main.go:97-119`): `cdm_planner`, `tcm_coordinator`,
`panel_manager`, `pre_visit_prep`, `inbox_triage`, `refill_manager`, `prior_auth`,
`engagement`, `readmission_tracker`. Injected state is one of `Registry`
(`medical.PatientStore`), `Tasks` (`tasks.TaskStore`), or `Notifier` (`engagement`). In
production these swap to the Postgres/Twilio-class adapters at construction — zero agent code
change.

### 9.4 Human-in-the-loop gate

Seven agents gate their clinical output behind `human_review` (`HITL: true`): `reasoning_verifier`
(`diagnosis_review`), `cdm_planner` (`care_plan_review`), `tcm_coordinator`
(`tcm_welcome_review`), `panel_manager` (`outreach_review`), `refill_manager` (`refill_review`),
`prior_auth` (`prior_auth_review`), `readmission_tracker` (`discharge_review`) — inventory at
`agents/human_review.go:63-73`. A gated agent emits a `review_request` carrying the HITL
envelope; `Queue.Submit` stores it `Pending` (`hitl/queue.go:111-148`). On decision,
`BuildResumeMessage` (`hitl/resume.go:30-52`) returns a resume message addressed to `NextTo`
with type `NextType` **only** on approve/modify; on reject/expire it returns `ok=false` and
**no** downstream message is produced — the "lossless gate" (no half-completed clinical action
escapes). Reject is logged as `KindReviewDecision`. This is the architectural expression of
the 21 USC §360j(o)(1) CDS carve-out (clinician can independently review the basis).

---

## 10. Observability, evaluation, and the autonomy ladder

**Eval mirror.** On a terminal supervisor diagnosis, the orchestrator scores the case
(`eval.ScoreCase`, `metrics.go:26-41`) into a `CaseMetrics` (diagnosis match, cost
utilization, step count) and saves an `InteractionRecord` (`eval.go:8-24`). An `LLMJudge`
(`judge.go`) can grade on four rubric dimensions (correctness, specificity, safety,
differential quality) with a pass threshold of 0.6.

**Graduation ladder** (`pkg/graduation`). Stages `development → shadow → copilot → autopilot`
(`stage.go:28-38`); promotion is one rung, gated on the **Wilson score lower bound** of the
success rate beating a threshold over a minimum count (default 0.95 / ≥100 obs, z=1.96,
`graduation.go:33-35`, `wilson.go`). The autopilot fence (I7) is checked first. **Critical
status:** this package has **no production caller** (ADR-0012), no persistence to accumulate
its own ≥100 observations, and does not gate CI — clinical accuracy is *not* a deploy gate
today (ADR-0010). CI gates on Go coverage and a bench *smoke* run without
`-fail-on-regression`; a prompt/model change that tanks accuracy ships green (ADR-0010,
ADR-0011). This is roadmap, not runtime.

---

## 11. The agent-decoupling substrate (Phase 0/1)

The active branch (`feat/agent-decoupling-phase0`, plan
`specs/002-agent-decoupling/plan.md`) is splitting the monolith so each agent can run as its
own process. The design keeps the *exact* per-message boundary the monolith enforces, so a
decoupled agent behaves identically.

**Phase 0 (complete): interface extraction.** `medical.PatientStore`, `tasks.TaskStore`,
`hitl.ReviewStore` extracted; agents bind to interfaces; Postgres adapters added; the last
`pkg/ → agents` import edge removed (zero `pkg/` file imports `agents`, prod or test) —
unblocking a future module split.

**Phase 1 (in flight): the per-agent safety boundary.** Three packages form it and must be
understood together:

- **`pkg/gateway`** — `Pipeline.Admit` (`pipeline.go:57-101`) reproduces Steps 1–2 of the
  orchestrator closure (governance evaluate/deny + audit invocation) as a reusable admission
  pipeline. Fail-closed on governance, fail-open on audit, never copies `msg.Content`.
  `Pipeline.RecordAgentError` mirrors the orchestrator's `KindAgentError` path so a decoupled
  agent's audit trail is identical.
- **`pkg/runtime`** — the per-agent message loop: `Run` subscribes the agent on its ID;
  `handle` calls `Pipeline.Admit` → `HandleMessage` → fan-out publish. It is the single-agent
  reassembly of the orchestrator closure (Steps 3+5); the eval mirror (Step 4) stays a
  standalone broadcast subscriber, not per-agent logic. Health probes (`/healthz`, `/readyz`)
  mirror `cmd/care`'s contract with pluggable `ReadyCheck`s.
- **`pkg/graduation`** — the autonomy ladder (§10), library-only for now.

**`pkg/comm` NATSBus** (§4.4) is the durable transport those processes would share
(at-least-once + DLQ, `-tags nats`), now green after the `938ab09` context-cancellation fix.
**`pkg/protocol`** is the shared message type both the monolith and any decoupled agent use.

**The open architectural tension (ADR-0007).** The plan proposes ~24 per-agent NATS services.
ADR-0007 (*Proposed*) argues that is premature distribution: there is *no security reason* to
isolate the cardiologist agent from the nephrologist (both stateless, secret-free, identical
trust profile), and a small team cannot operate 24 services (Conway's law). Its recommendation
is a **trust-tier split (3–4 services)** — public backend / stateless-rule agents /
secret-holding LLM agents / state-mutating agents — banking the Phase-0 modularization without
paying for per-agent distribution. And ADR-0009 warns that turning on at-least-once redelivery
while messages still carry fat mutable `CaseState` risks silent case corruption (a redelivered
stale snapshot overwriting newer state, with no `version` check today) — so the "thin events +
Postgres system-of-record" refactor must land *before* the durable bus carries clinical
traffic. These are authored constraints, not speculation.

---

## 12. Concurrency and ordering

`InMemoryBus` gives no per-recipient FIFO; two racing `Publish` calls can invoke a recipient's
handlers in scheduler order. Ordering in the diagnostic flow is done by the **application
protocol** — the supervisor waits for `questions_complete` before `test_planner`,
`tests_complete` before `diagnostician` — not by the bus (I5/§9.1). Care-services fan-in
points work today because their handlers are idempotent. Production options: a per-recipient
single-consumer FIFO (total order, head-of-line blocking risk) or causal metadata +
idempotent handlers (heavier per-message, survives retries). The durable bus's at-least-once
semantics make **idempotency a requirement**, not a nicety — reinforced by the ADR-0009
lost-update hazard.

Policy state: policies are stateless *except* `RateLimitPolicy` (mutex-guarded token buckets).
Planned stateful policies (BreakGlass windows, Part2 consent caches) are to receive a bound
cache via context, never the live audit log.

---

## 13. Failure model

| Failure | Behaviour | Site |
|---|---|---|
| Agent panic | Recovered by `safeHandle`; logged; other agents continue; NATS Naks toward DLQ | `bus.go:277-294` |
| `HandleMessage` error | `KindAgentError` (err text only); outputs dropped; case stalls (supervisor gets no follow-up) | `orchestrator.go:315-323` |
| `policy.Evaluate` error | Treated as deny; logged; **not audited**; agent never sees message | `orchestrator.go:260-267` (I1 hole) |
| `policy` deny | Audited (`KindPolicyDecision`); agent never sees message | `orchestrator.go:269-277` |
| LLM failure (timeout/JSON/low-conf) | Deterministic rule fallback; case finalizes | `diagnostician_llm.go:203-234` (I6) |
| Audit sink error | Logged via `env.Logf`; processing continues (fail-open) | `orchestrator.go:387` (I2) |
| Postgres open failure at boot | Silent fallback to in-memory | `main.go:725-729` |
| `BODH_ENCRYPTION_KEY` unset | Encrypt is a no-op; PHI stored plaintext | `crypto.go:54-71` |
| Bus crash (in-memory) | In-flight messages lost (no persistence) | `bus.go:85-106` |

The recurring theme: failures degrade toward *availability* (fail-open audit, in-memory
fallback, LLM fallback). For a research substrate that is the right default; for a clinical
product, the fail-open audit and silent persistence fallback are the two that must flip to
fail-loud (ADR-0008), and the audit fail-open now already flips to fail-loud in one place —
the "swallowed clinical write errors" fix (commit `fb7ccff`, audit findings F1/F2).

---

## 14. Threat model (trust boundaries from §2)

| TB | Threat | Current posture | Mitigation / roadmap |
|---|---|---|---|
| TB1 ingress | Unauthenticated access to clinical routes | **No auth wired by default** (ADR-0006) | `pkg/auth` (OPA+JWT), `SMARTScopePolicy` exist unwired; wire before any non-demo use |
| TB2 governance | Spoofed `tenant_id`/`scopes` (no identity binding) | Client-asserted metadata trusted | Bind identity at TB1, then `RequireTenant`/`SMARTScope` become meaningful |
| TB3 bus | Redelivered stale fat message overwrites newer state | Real hazard under NATS at-least-once | Thin events + `version` optimistic concurrency (ADR-0009) **before** durable bus |
| TB4 audit | Incomplete or mutable audit trail | Fail-open + mutable Postgres table (no chain-hash) | Fail-loud for clinical types + wire `ImmutableRecorder` or add hash columns (ADR-0008) |
| TB5 LLM egress | PHI sent to third-party model | `Content` leaves process on LLM calls | De-identify before egress; prompt-injection scored by `AgentMeshGate` |
| TB6 persistence | Cross-tenant read; plaintext PHI at rest | RLS forced (strong); encryption key-gated | RLS verified at runtime; **require** `BODH_ENCRYPTION_KEY` in deployment |

The honest summary: TB6 (RLS) is genuinely strong; TB4 and TB1 are the two boundaries where
the running binary is furthest from the documented intent, and both are owned by open ADRs.

---

## 15. Known gaps and roadmap (the register)

Every item here is an ADR-authored admission, not external critique. This section exists so a
reviewer cannot "catch" a gap the project has not already documented.

| # | Gap | Owner ADR | Status |
|---|---|---|---|
| G1 | Project identity (research vs clinical) undecided — root of the other gaps | 0006 | Needs-Decision; review leans "staged research platform today" |
| G2 | Audit is fail-open at every seam; §164.312(b) completeness not guaranteed | 0008 | Open (partial fix `fb7ccff` makes swallowed clinical *write* errors loud) |
| G3 | Tamper-evident `ImmutableRecorder` (ADR-0002, Accepted) wired nowhere; prod audit is mutable Postgres INSERT | 0002/0008 | Drift from an Accepted decision; must re-affirm or supersede |
| G4 | Fat mutable `CaseState` on the wire + at-least-once ⇒ lost-update/case-corruption risk | 0009 | Proposed; must precede durable-bus clinical traffic |
| G5 | 24 per-agent services likely over-distributed; trust-tier split (3–4) recommended | 0007 | Proposed; owner decision pending |
| G6 | `pkg/graduation` has no production caller/persistence; clinical accuracy not a deploy gate | 0010/0012 | Needs-Decision; enable `-fail-on-regression`, wire the ladder |
| G7 | Model/prompt changes ship via env var with no CI signal, no version, no audit | 0011 | Accepted remediation (allowlist + prompt versioning + eval gate) not yet built |
| G8 | No auth middleware wired in `cmd/care` | 0006 | Roadmap; `pkg/auth` exists |
| G9 | Encryption at rest is opt-in (`BODH_ENCRYPTION_KEY`); plaintext if unset | — | Operator requirement; consider fail-closed on unset in prod builds |

---

## 16. Decision log

The design rationale lives in [docs/adr/](adr/). `0001`–`0005` are foundational; `0006`–`0012`
are a 2026 principal-engineer review whose resolution is gated by ADR-0006.

| ADR | Title | Status |
|---|---|---|
| 0001 | LLM abstraction layer | Accepted |
| 0002 | Chain-hash audit trail | Accepted (unadopted in prod wiring — G3) |
| 0003 | Build tags for optional features | Accepted |
| 0004 | MARA pipeline architecture | Accepted |
| 0005 | PHI de-identification | Accepted |
| 0006 | Project identity (research vs clinical) | **Needs-Decision** — read first |
| 0007 | Deployment topology (per-agent vs trust-tier) | Proposed |
| 0008 | Harden substrate before distributing | Needs-Decision |
| 0009 | Thin events + Postgres system-of-record | Proposed |
| 0010 | Evals as deploy gate | Needs-Decision |
| 0011 | Build and model governance | Accepted (remediation pending) |
| 0012 | Autopilot is a regulated change | Accepted |

---

## 17. Extension recipes

**Add an agent.** Implement `agent.Agent` in `agents/<name>.go`; declare honest
`Capabilities()`; define input/output `Type`s; table-driven test; register in
`cmd/care`/`cmd/demo`/`cmd/caredemo`; update the supervisor if it routes the new type. The
orchestrator closure gives routing, governance, audit, and observability for free (I5).

**Add a policy.** Implement `governance.Policy`; add it to the `NewComposite(...)` list in
`cmd/care/main.go:71-77` at the right position (cheapest/most-discriminating first). No
orchestrator change. Keep it PHI-free in `Reason` (it lands in audit).

**Add a condition.** Extend `medical.BuildCarePlan`, `DefaultPanelRules`, `test_planner`'s
condition→workup map, and `diagnostician.inferDiagnosis`; add a `data/cases/` fixture for the
bench. Zero platform change — the composition pattern working as intended.

---

*This document reflects the code on branch `feat/agent-decoupling-phase0`. When it and an ADR
disagree, the code — and this document — win; the ADR is a snapshot of intent at its date.
Claims are anchored to `file:line` so you can verify rather than trust.*
