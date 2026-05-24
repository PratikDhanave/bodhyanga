# Bodh — Architecture Deep Dive

> **Audience.** Engineers building on or extending Bodh. Assumes you've read the [main README](../README.md) for context.

> **Companion docs.** [agents.md](agents.md) for per-agent behaviour, [api.md](api.md) for HTTP surface, [governance.md](governance.md) for policy details, [data-model.md](data-model.md) for clinical types, [operations.md](operations.md) for audit/observability, [deployment.md](deployment.md) for production layout.

---

## Contents

1. [The one-sentence model](#the-one-sentence-model)
2. [MARA building blocks](#mara-building-blocks)
3. [The five core interfaces](#the-five-core-interfaces)
4. [Message protocol](#message-protocol)
5. [Orchestrator: the only place where governance runs](#orchestrator-the-only-place-where-governance-runs)
6. [Bus semantics](#bus-semantics)
7. [Registry semantics](#registry-semantics)
8. [Environment and Logger](#environment-and-logger)
9. [Sequential diagnosis state machine](#sequential-diagnosis-state-machine)
10. [Care-services orchestration](#care-services-orchestration)
11. [Human-in-the-loop pattern](#human-in-the-loop-pattern)
12. [LLM agent fallback contract](#llm-agent-fallback-contract)
13. [Agentic patterns: Debate, Reflexion, Cascade](#agentic-patterns-debate-reflexion-cascade)
14. [Audit pipeline](#audit-pipeline)
15. [Persistence layer](#persistence-layer)
16. [Failure modes and recovery](#failure-modes-and-recovery)
17. [Concurrency and ordering](#concurrency-and-ordering)
18. [Design rules from MARA](#design-rules-from-mara)
19. [Adding a new agent](#adding-a-new-agent)
20. [Adding a new policy](#adding-a-new-policy)
21. [Adding a new condition](#adding-a-new-condition)

---

## The one-sentence model

A user (or API call) publishes a clinical message → the orchestrator delivers it to the right agent → the agent returns zero or more new messages → the orchestrator publishes those → **governance gates each hop** → the workflow continues until a terminal agent decides we're done.

Every clinically interesting behaviour in Bodh is a consequence of that loop. The clinical specificity lives in **agents** and **policies**, never in the orchestrator.

---

## MARA building blocks

Bodh implements Microsoft's [Multi-Agent Reference Architecture (MARA)](https://microsoft.github.io/multi-agent-reference-architecture/). Each MARA concept maps to a single Go package:

| MARA concept | Go package | Key type |
|---|---|---|
| Message / protocol | `pkg/protocol` | `Message` |
| Agent contract | `pkg/agent` | `Agent`, `Environment` |
| Agent registry | `pkg/registry` | `Registry`, `InMemoryRegistry` |
| Bus / transport | `pkg/comm` | `Bus`, `InMemoryBus` |
| Orchestrator | `pkg/orchestration` | `Orchestrator`, `SimpleEnvironment` |
| Governance | `pkg/governance` | `Policy`, `CompositePolicy` |
| Memory | `pkg/memory` | `KeyValueStore`, `InMemoryKV` |
| Observability | `pkg/observability` | `Logger`, `Clock`, `LLMTrace` |
| Evaluation | `pkg/eval` | `InteractionRecord`, `CaseMetrics`, `Store`, `LLMJudge` |
| Audit | `pkg/audit` | `Event`, `Recorder` |
| PHI redaction | `pkg/phi` | `Redactor`, `LoggerWrapper` |
| HITL gate | `pkg/hitl` | `Queue`, `ReviewRequest` |

Five additional packages serve the medical domain layer (`pkg/medical`, `pkg/tasks`, `pkg/engagement`, `pkg/fhir`, `pkg/hl7v2`, `pkg/analytics`, `pkg/persistence`, `pkg/safety`, `pkg/llm`, `pkg/embeddings`, `pkg/knowledge`, `pkg/tools`). They don't define new MARA concepts — they're the clinical content the agents operate on.

---

## The five core interfaces

Everything in Bodh — 17+ agents, the bus, the registry, the governance composer, the HITL gate — composes from these five contracts.

### `Agent`

```go
type Agent interface {
    ID() string
    Name() string
    Capabilities() []string
    HandleMessage(ctx context.Context, msg Message, env Environment) ([]Message, error)
}
```

- `ID()` is the addressable name on the bus (e.g. `"diagnostic_supervisor"`, `"intake"`). It's used as the `To` field for routing.
- `Name()` is a human label for logs and UI.
- `Capabilities()` is a list of capability strings the registry can search by (`"ask_question"`, `"verify_reasoning"`, `"order_test"`). Useful when a supervisor wants to find any agent that can answer a particular capability.
- `HandleMessage` is the work: take one message, return zero or more outputs. The orchestrator handles routing those outputs back to the bus.

### `Bus`

```go
type Bus interface {
    Subscribe(agentID string, h Handler) (unsubscribe func())
    Publish(ctx context.Context, msg protocol.Message)
}

type Handler func(ctx context.Context, msg protocol.Message)
```

- `Subscribe` registers a handler for messages addressed to a specific agent ID. Pass `""` as the agent ID to receive broadcasts (e.g. for observability sinks).
- `Publish` routes by `msg.To`. The in-memory bus invokes matching handlers as async goroutines.
- The returned `unsubscribe` function removes the handler. Useful for tests and dynamic agent lifecycle.

The default `InMemoryBus` is for single-process demos. Production swaps Kafka / NATS JetStream / RabbitMQ behind this interface without any agent code changes. See [operations.md](operations.md#bus-implementations).

### `Registry`

```go
type Registry interface {
    Register(ctx context.Context, a agent.Agent) error
    Get(ctx context.Context, id string) (agent.Agent, error)
    List(ctx context.Context) []agent.Agent
    FindByCapability(ctx context.Context, capability string) []agent.Agent
}
```

- `Register` adds an agent. The in-memory registry's "register replaces" semantics — registering twice with the same ID overwrites.
- `List` is what `Orchestrator.Start` calls to snapshot the agent set.
- `FindByCapability` returns all agents whose `Capabilities()` includes the requested string. Used by `Orchestrator.Refresh` and any agent that wants to dispatch to a capability rather than a hard-coded peer.

### `Policy`

```go
type Policy interface {
    Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error)
}

type PolicyResult struct {
    Decision  Decision     // DecisionAllow | DecisionDeny
    Reason    string
    CheckedAt time.Time
}
```

- `Evaluate` runs for every message before any agent sees it. Errors are treated as policy failures (deny).
- `CompositePolicy` denies if any child denies (fail fast). The composed list is the project's safety boundary.
- Today's shipped policies: `MaxContentLengthPolicy`, `RequireCaseIDPolicy`, `MaxDiagnosticCostPolicy`, `ResearchDisclaimerPolicy`, `RequireTenantPolicy`, `RequireScopePolicy`. See [governance.md](governance.md) for each one's contract.

### `Environment`

```go
type Environment interface {
    Now() time.Time
    Logf(format string, args ...any)
}
```

Deliberately minimal. The orchestrator hands the same `Environment` to every agent's `HandleMessage`. Extend by composition — add fields to your concrete type, give it richer methods, then type-assert in the agent when you need them. This pattern keeps the interface stable while letting deployments inject memory, LLM clients, FHIR clients, secret stores, KMS clients, etc.

The shipped `SimpleEnvironment`:

```go
type SimpleEnvironment struct {
    Logger observability.Logger
    Clock  observability.Clock
}
```

Replace `Logger` with the PHI-redacting `phi.LoggerWrapper` for production. Replace `Clock` with a deterministic test clock in unit tests.

---

## Message protocol

Every coordination event in Bodh is a `protocol.Message`. The wire format is the same for diagnostic flows, care services, HITL reviews, and HL7 ingest.

```go
type Message struct {
    ID        string         // UUID — audit / causal graph
    From      string         // sender agent id or "user"
    To        string         // recipient agent id (routing address)
    Role      MessageRole    // user | system | agent | tool | observer | evaluator
    Type      string         // handler branch
    Content   string         // primary payload (JSON for clinical data)
    CreatedAt time.Time      // UTC
    Metadata  map[string]any // case_id, patient_id, trace_id, costs, ...
}
```

### Roles

| Role | Use |
|---|---|
| `user` | Initial input — a `presentation` from a clinician or API client |
| `agent` | Internal coordination — every agent-to-agent message |
| `tool` | Future: MCP tool calls (lab feed, imaging AI, RxNorm lookup) |
| `system` | Configuration / policy broadcasts |
| `observer` | Audit / telemetry events |
| `evaluator` | Rubric scores from `eval.LLMJudge` |

### Metadata conventions

| Key | Required when | Purpose |
|---|---|---|
| `case_id` | All clinical types | Correlation; **never** log MRNs in clear |
| `patient_id` | Care-services types | Population correlation |
| `tenant_id` | Multi-tenant deployments | Required by `RequireTenantPolicy`; carried into audit events for RLS |
| `trace_id` | Recommended | Distributed tracing — propagates into `LLMTrace` and OTel spans |
| `max_cost_usd` | Intake / case budget | SD-Bench cost ceiling |
| `gold_diagnosis` | Benchmarks only | Offline eval — never present in production messages |
| `spent_usd`, `test_cost_usd` | Test ordering | Per-message budget check by `MaxDiagnosticCostPolicy` |
| `scopes` | SMART-on-FHIR | Space-separated OAuth scopes for `RequireScopePolicy` |
| `inbox_category` | Inbox routing | Triage decision recorded for audit |
| `hitl_kind`, `hitl_next_to`, `hitl_next_type`, `hitl_priority`, `hitl_subject`, `hitl_sla_minutes` | HITL gate | See [HITL pattern](#human-in-the-loop-pattern) |
| `image_path` | Vision diagnostician | Local path to attach to LLM message |

### Message types

See [api.md](api.md#message-types) for the full catalog. Headline categories:

- **Diagnostic flow:** `presentation`, `case_state`, `questions_complete`, `tests_complete`, `diagnosis_proposal`, `diagnosis`, `order_test`, `ask_question`, `verify_reasoning`
- **Care services:** `cdm_request`, `care_plan_ready`, `discharge_event`, `tcm_tasks_created`, `panel_request`, `panel_report`, `engagement_request`, `pre_visit_request`, `pre_visit_ready`, `inbox_message`, `inbox_routed`, `refill_request`, `refill_response`, `prior_auth_request`, `prior_auth_status`, `adt_event`, `readmission_risk`, `engagement_sent`
- **HITL:** `review_request`
- **Ingest:** ADT messages via HL7 v2 land as `adt_event` after parsing

---

## Orchestrator: the only place where governance runs

The orchestrator's single most important method is the subscription closure that wires each agent to the bus. Here's the full implementation:

```go
func (o *Orchestrator) subscribeAgentLocked(a agent.Agent) {
    agentID := a.ID()
    o.bus.Subscribe(agentID, func(c context.Context, msg agent.Message) {
        // 1. Apply governance BEFORE the agent sees the message.
        if o.policy != nil {
            res, err := o.policy.Evaluate(c, msg)
            if err != nil {
                o.env.Logf("policy error for msg %s: %v", msg.ID, err)
                o.recordAudit(audit.Event{ /* policy error */ })
                return
            }
            if res.Decision == governance.DecisionDeny {
                o.env.Logf("message %s denied by policy: %s", msg.ID, res.Reason)
                o.recordAudit(audit.FromPolicyDecision(msg, agentID, res))
                return  // agent NEVER sees the message
            }
            // Allow recorded for clinical types only (signal-rich)
            if isClinicalType(msg.Type) {
                o.recordAudit(audit.FromPolicyDecision(msg, agentID, res))
            }
        }

        // 2. Record the agent invocation.
        o.env.Logf("agent %s handling message %s from %s", agentID, msg.ID, msg.From)
        o.recordAudit(audit.FromMessage(msg, agentID))

        // 3. Delegate to the agent implementation.
        out, err := a.HandleMessage(c, msg, o.env)
        if err != nil {
            o.env.Logf("agent %s error: %v", agentID, err)
            o.recordAudit(audit.Event{
                Kind:    audit.KindAgentError,
                AgentID: agentID,
                ErrorText: err.Error(),  // err message ONLY — never msg.Content
            })
            return
        }

        // 4. Republish outputs. One agent's output becomes another's input.
        for _, m := range out {
            o.bus.Publish(c, m)
        }

        // 5. Side-channel: if supervisor finalizes a case, score and store.
        if agentID == "diagnostic_supervisor" && msg.Type == "diagnosis" && len(out) == 0 {
            o.recordEvalIfPossible(msg)
        }
    })
    o.subscribed[agentID] = struct{}{}
}
```

Five properties fall out of this design:

1. **Fail closed.** A denied message has no path that reaches an agent. There is no fallback that bypasses policy.
2. **Single audit point.** Every governance decision and every agent invocation passes through this closure. Want a HIPAA audit log? Instrument here once, not in 17 agents.
3. **Pluggability.** Want multi-tenancy, scope checks, rate limiting, or 42 CFR Part 2 SUD segmentation? Add a `Policy`. The orchestrator doesn't change.
4. **No clinical logic.** The orchestrator never branches on diagnosis, condition, or test results. It's pure transport + policy.
5. **Audit is fail-open.** If `recorder.Record()` errors, log via `env.Logf` and continue — never block message processing because audit failed. Audit failures are operational issues; blocking the diagnostic flow because the audit sink is down is the worst outcome.

### Lifecycle

```go
orch := orchestration.NewOrchestrator(reg, bus, policy, env)

// Optional: enable audit + eval persistence (after PR #12)
orch.SetAuditRecorder(audit.NewInMemory())
orch.SetEvalStore(eval.NewMemoryStore())

orch.Start(ctx)               // snapshot registry, subscribe everyone
go orch.Watch(ctx, time.Second)  // optional: hot-reload new registrations
```

- `Start` is idempotent via `sync.Once`. Calling it twice is a no-op.
- `Refresh` re-lists the registry and subscribes any agents the orchestrator doesn't yet know about. Used by `Watch` and by callers that just registered a new agent.
- `Watch` runs `Refresh` on an interval; logs newly-subscribed agent IDs.
- Setters (`SetAuditRecorder`, `SetEvalStore`) take effect for **future subscriptions only** — the closures capture the recorder at subscribe time. Call them before `Start`.

---

## Bus semantics

### `InMemoryBus` (default, demo-grade)

- Each `Publish(msg)` looks up subscribers for `msg.To` and invokes each handler in a **separate goroutine**.
- Handler panics are recovered (added in commit `1b8ce62`) so one buggy agent can't crash the process.
- `agentID == ""` subscriptions are broadcast — they receive every message that's published.
- **No FIFO guarantee.** If Agent A and Agent B both emit to Agent C as part of one trigger, C's handler is invoked twice in goroutine-scheduling order. Today the application protocol (state machines in supervisors) does the ordering work — see [Concurrency and ordering](#concurrency-and-ordering).

### Production swaps

| Implementation | When | Ordering | Notes |
|---|---|---|---|
| `InMemoryBus` | Demo, single-process | None per recipient | Default |
| Kafka topic per recipient | High throughput, multi-process | FIFO per partition | Pin one consumer per partition for total order |
| NATS JetStream WorkQueue | Mid-scale, cloud-native | FIFO with `max_ack_pending=1` | Cleaner ops than Kafka for small fleets |
| RabbitMQ direct exchange | Existing Rabbit footprint | FIFO per queue | Per-recipient queue required |
| GCP Pub/Sub | GCP-native | None inherent | Add ordering keys + single subscriber per key for FIFO |
| AWS EventBridge | AWS-native | None | Bus to Lambda fan-out; ordering via separate FIFO SQS |

The swap is a `Bus` implementation. Zero agent code changes. See [operations.md](operations.md#bus-implementations) for setup details.

---

## Registry semantics

`InMemoryRegistry` is a thread-safe map keyed on agent ID with a secondary capability index.

- **Hot reload.** Register an agent after `Orchestrator.Start` and call `Orchestrator.Refresh` (or rely on `Watch`) to subscribe it.
- **No removal.** `InMemoryRegistry.Register` replaces; there's no `Unregister`. Removing an agent from a running orchestrator is a deliberate non-feature — tearing down a live subscription with messages in flight is delicate. Planned for a future iteration.
- **Capability lookup.** `FindByCapability("ask_question")` returns all agents that publish `"ask_question"` in their `Capabilities()`. The `inbox_triage` agent uses this to find a primary-care agent at runtime — capability-driven dispatch, not hard-coded peers.

Production: replace the registry with a service-discovery-backed implementation (Consul, etcd, Kubernetes EndpointSlice) for multi-process deployments where agents live in separate pods.

---

## Environment and Logger

The `Environment` interface is intentionally minimal so it never breaks agent contracts when richer concrete types are added.

### Logger composition

```go
// Default: prints "[bodh] ..." to stdout
inner := observability.NewStdLogger()

// Production: PHI-redact before any third-party sink
redacted := &phi.LoggerWrapper{
    Redactor: phi.NewRedactor(),
    Inner:    inner,
}

env := &orchestration.SimpleEnvironment{
    Logger: redacted,
    Clock:  observability.SystemClock{},
}
```

The PHI redactor applies six default patterns (SSN, MRN, US phone, email, ISO date, US date) — see [governance.md](governance.md#phi-redaction). Add custom patterns for your deployment's internal MRN formats.

### Clock injection

```go
type Clock interface { Now() time.Time }

type SystemClock struct{}
func (SystemClock) Now() time.Time { return time.Now().UTC() }

type FakeClock struct { T time.Time }
func (f *FakeClock) Now() time.Time { return f.T }
```

Tests use `FakeClock` for deterministic timestamps. Agents call `env.Now()` instead of `time.Now()` for the same reason.

### LLMTrace hook

Every `llm.Client.Complete` call produces an `observability.LLMTrace`. Install a hook with `observability.SetLLMOTelHook(fn)` to forward to OpenTelemetry — see [operations.md](operations.md#opentelemetry-export).

---

## Sequential diagnosis state machine

The diagnostic flow runs 8 agents through 12 messages per case. The supervisor is a pure state machine on `msg.Type`:

```
incoming type            → routed to
─────────────────────────────────────────────
case_state (from intake) → questioner
questions_complete       → test_planner
tests_complete           → diagnostician
diagnosis (verified)     → terminal (score + finalize)
```

### Full trace for `demo-case-001`

| Step | From | To | Type | Notes |
|---|---|---|---|---|
| 1 | `user` | `intake` | `presentation` | chief complaint + HPI |
| 2 | `intake` | `rpm_monitor` | `case_state` | no RPM observations, passthrough |
| 3 | `rpm_monitor` | `diagnostic_supervisor` | `case_state` | |
| 4 | `supervisor` | `questioner` | `case_state` | start history |
| 5 | `questioner` | `supervisor` | `questions_complete` | 4 Q&A, 3 hypotheses |
| 6 | `supervisor` | `test_planner` | `case_state` | order labs/imaging |
| 7 | `test_planner` | `cost_guardian` | `case_state` | total \$220 |
| 8 | `cost_guardian` | `supervisor` | `tests_complete` | budget OK (\$220 / \$500) |
| 9 | `supervisor` | `diagnostician` | `case_state` | infer |
| 10 | `diagnostician` | `reasoning_verifier` | `diagnosis_proposal` | CAP @ 87% |
| 11 | `reasoning_verifier` | `supervisor` | `diagnosis` | verified |
| 12 | `supervisor` | — | (terminal) | score + finalize |

12 messages = 12 audit events = 12 governance evaluations. No hidden state, no inter-agent function calls, no place where a clinician can't intervene.

### Cost-aware rework

When `cost_guardian` denies (e.g. `spent_usd + test_cost_usd > max_cost_usd`):

1. `cost_guardian` emits no `tests_complete` message.
2. Instead it emits `tests_revise` with a budget-aware list of remaining tests.
3. `diagnostic_supervisor.reviseTests` accepts and reroutes to `test_planner` with the constraint.
4. Loop continues until the workup fits the budget or `MAX_REWORK_ROUNDS` (default 3) is reached.

### Specialty dispatch (PR #11)

After PR #11, `test_planner` switches on `state.Condition`:

| Condition | Workup |
|---|---|
| `cap` / default | CBC + CXR + BMP |
| `chf` | BNP + CXR + ECG + BMP |
| `acs` | troponin_i + ECG + BMP + CBC |
| `sepsis` | blood_culture + lactate + CBC + BMP + urinalysis |
| `dka` | BMP + urine_ketones + ABG + CBC |

`diagnostician.inferDiagnosis` interprets the results with specialty-specific rules. See [agents.md](agents.md#diagnostician).

---

## Care-services orchestration

The care-services layer (9 agents) shares the same bus, same registry, same governance. Agents subscribe to specific message types and share **dependency-injected state** through struct fields:

```go
type CDMPlannerAgent struct {
    PatientRegistry *medical.PatientRegistry  // injected by cmd/care or cmd/caredemo
    TaskQueue       *tasks.Queue
}
```

Shared state today:
- `medical.PatientRegistry` — population roster (CDM/TCM/panel/refill/readmission read & write)
- `tasks.Queue` — nurse work queue (TCM/inbox/refill/PA write; HTTP `GET /tasks` reads)
- `engagement.Notifier` — SMS/email/IVR sink (engagement writes; HTTP `GET /notifications` reads)

These are wired in `cmd/care/main.go`. In production they swap to:
- `medical.PatientRegistry` → `*pkg/persistence/postgres.PatientRegistryAdapter` (per-tenant, RLS-enforced)
- `tasks.Queue` → Postgres-backed durable queue
- `engagement.Notifier` → Twilio / SendGrid / IVR provider (under BAA)

The substitution is at construction time. Zero agent code changes.

---

## Human-in-the-loop pattern

The `human_review` agent is the architectural expression of [21 USC §360j(o)(1)](https://www.law.cornell.edu/uscode/text/21/360j) — the Clinical Decision Support carve-out criterion 4: "enable [the clinician] to independently review the basis for the recommendations."

### Envelope keys

| Key | Required | Purpose |
|---|---|---|
| `hitl_kind` | yes | Review category (`diagnosis_review`, `care_plan_review`, etc.) |
| `hitl_next_to` | yes | Agent to forward to on approval |
| `hitl_next_type` | yes | Message type used on resume |
| `hitl_priority` | no | `low` / `normal` / `high` / `urgent` (default `normal`) |
| `hitl_subject` | no | Short label for the queue UI |
| `hitl_sla_minutes` | no | Deadline; expired reviews never resume |

### Flow

```
upstream agent ──► human_review ──► (pending in queue)
                                      │
                          reviewer decides (HTTP / API)
                                      │
                          ┌───────────┴───────────┐
                          │                       │
                      approve                  reject
                          │                       │
                          ▼                       ▼
                  next agent (resume)      (no resume; logged)
```

### Gated agents (HITL=true)

| Agent | Gated output | Review kind |
|---|---|---|
| `reasoning_verifier` | Verified diagnosis | `diagnosis_review` |
| `cdm_planner` | Care plan | `care_plan_review` |
| `tcm_coordinator` | TCM welcome message | `tcm_welcome_review` |
| `panel_manager` | Per-gap outreach | `outreach_review` |
| `refill_manager` | Refill response | `refill_review` |
| `prior_auth` | PA status | `prior_auth_review` |
| `readmission_tracker` | Discharge handoff (priority scales with risk band) | `discharge_review` |

### HTTP endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/reviews` | All reviews. `?status=pending` to filter |
| `GET` | `/reviews/{id}` | Single review |
| `POST` | `/reviews/{id}/decide` | `{reviewer_id, decision, reason, modification}` — `decision` ∈ `approve` / `approve_with_modifications` / `reject` |

### Lossless on reject

On `reject`, the gated payload is logged as a `KindReviewDecision` audit event and dropped. **No half-completed clinical action can leak past the gate.**

### Cascading approvals

Approvals trigger downstream agents that may themselves emit `review_request` envelopes. Example chain:

```
readmission_tracker emits discharge_review
  → reviewer approves
    → tcm_coordinator handles discharge_event
      → emits tcm_welcome_review
        → reviewer approves
          → engagement sends welcome SMS
```

The `cmd/caredemo` auto-approver drains the queue across rounds to demonstrate. Production UIs show cascaded reviews as they appear.

---

## LLM agent fallback contract

Every LLM-backed agent in Bodh has a **deterministic rule-based fallback**.

```go
type LLMDiagnostician struct {
    Client        llm.Client
    Fallback      *DiagnosticianAgent   // always present
    MinConfidence float64               // below this, fall back
}
```

Failure modes that route to the fallback:

| Trigger | Behaviour |
|---|---|
| Network timeout / transport error | Fall back |
| Malformed JSON from model | Fall back |
| Confidence below `MinConfidence` | Fall back |
| Hallucination check fails (RAG grounding insufficient) | Fall back |
| Safety guardrail trips (refusal phrase, PHI pattern in output) | Fall back |
| Latency budget exceeded | Fall back |

The case **always finalizes**. The audit log records which path was taken (via the `[llm-trace]` JSON line and the resulting `audit.Event`'s `agent_id`). Operators can graph LLM-success-rate vs fallback-rate by agent — that's the SLO that matters.

This isn't pessimism about LLMs. It's the same circuit-breaker shape distributed systems already use: degrade gracefully, never break loudly. See [operations.md](operations.md#llm-reliability) for SLO recommendations.

---

## Agentic patterns: Debate, Reflexion, Cascade

Three multi-call patterns ship alongside the single-call `LLMDiagnostician`. All share the same fallback contract and the same `agents/diagnostician_factory.go`.

### Single (baseline)
One `llm.Client.Complete` call. Lowest latency, lowest cost.

### Debate (`DebateDiagnostician`)
Multiple "specialist" personas (cardiologist, pulmonologist, infectious-disease) each produce a `DiagnosisProposal` in parallel. A "chair" agent synthesises a final answer from their proposals + rationales. Higher cost; useful when disagreement signal matters.

### Reflexion (`ReflexionDiagnostician`)
A self-critique loop: produce → critique → revise. Up to N rounds (default 2). Each round is a separate LLM call. Stops early when the critique returns "no objections."

### Cascade (`CascadeDiagnostician`)
A cheap small model tries first. If confidence < threshold, escalate to a larger model. Optimises mean cost while preserving accuracy on the hard cases.

Pick via CLI:

```bash
go run ./cmd/demo -diagnostician=llm-anthropic-debate
go run ./cmd/demo -diagnostician=llm-openai-reflexion
go run ./cmd/demo -diagnostician=llm-ollama-cascade
go run ./cmd/bench -providers=rule,llm-anthropic,llm-anthropic-debate -manifest=data/cases/manifest.json
```

All three are factory-selected with their fallback chain intact. Run them with `cmd/bench` to compare on the Pareto frontier.

---

## Audit pipeline

After PR #12, every clinical message produces audit events. The pipeline:

```
agent layer
    │
    ▼
orchestrator.subscribeAgentLocked
    │
    ├──► policy.Evaluate ──► audit.Event{KindPolicyDecision, ...}
    │
    ├──► agent.HandleMessage ──► audit.Event{KindMessage, ...}
    │                       └──► (on err) audit.Event{KindAgentError, ...}
    │
    └──► (terminal diagnosis) eval.Store.Save(InteractionRecord)
                                    │
                                    ▼
                           audit.Recorder.Record (in-memory OR Postgres)
                                    │
                                    ▼
                           pkg/persistence/postgres.audit_events table
                           (RLS-enforced, SELECT+INSERT only — DB grants)
```

### Event schema

```go
type Event struct {
    ID         string    `json:"id"`
    At         time.Time `json:"at"`
    Kind       Kind      `json:"kind"`         // message | policy_decision | review_decision | agent_error
    AgentID    string    `json:"agent_id,omitempty"`
    MessageID  string    `json:"message_id,omitempty"`
    From       string    `json:"from,omitempty"`
    To         string    `json:"to,omitempty"`
    Type       string    `json:"type,omitempty"`
    TenantID   string    `json:"tenant_id,omitempty"`
    PatientID  string    `json:"patient_id,omitempty"`  // opaque ID, NOT MRN
    CaseID     string    `json:"case_id,omitempty"`
    TraceID    string    `json:"trace_id,omitempty"`
    Decision   string    `json:"decision,omitempty"`
    Reason     string    `json:"reason,omitempty"`
    ReviewerID string    `json:"reviewer_id,omitempty"`
    ErrorText  string    `json:"error,omitempty"`
}
```

What's **deliberately absent:** no narrative, no diagnosis text, no patient name, no DOB, no test result content. The event is a **pointer** into the system — enough to reconstruct who-did-what-when, never enough to leak PHI if the audit sink is ever compromised. See [governance.md](governance.md#audit-events) for the field-selection rationale.

### Backends

| Backend | When | Properties |
|---|---|---|
| `audit.NewInMemory()` | Tests, demos | Lost on restart |
| `audit.NewFileStore(path)` | Single-host pilots | Append-only JSON-lines |
| `*pkg/persistence/postgres.Store.AuditRecorder(tenantID)` | Production | RLS, SELECT+INSERT only at DB grants, tenant-isolated |

Production target adds a WORM sink (S3 Object Lock, Azure Immutable Blob) + Merkle chain-hash for tamper evidence. See [operations.md](operations.md#audit-pipeline).

---

## Persistence layer

`pkg/persistence/postgres` (PR #10) implements:

- `eval.Store` — `Save(InteractionRecord)`, `List()`
- `audit.Recorder` — `Record(Event)`, `List()`
- `medical.PatientRegistry` adapter — `Upsert`, `Get`, `List`, `FilterByCondition`
- Tenant-aware variants on `*Store`: `SaveInteraction`, `UpsertPatient`, `GetPatient`, `ListPatients`, `FilterPatientsByCondition`, `DeletePatient`, `RecordAuditEvent`, `ListAuditEvents`

### Row-level security model

Every table has a `tenant_id` column. The `bodh_app` role is created with `NOBYPASSRLS`. Every connection sets `app.tenant_id` via `set_config(..., true)` in a per-statement transaction (the `withTenant` helper). The RLS policy is:

```sql
CREATE POLICY tenant_isolation ON interaction_records
    USING (tenant_id = current_setting('app.tenant_id', true));
```

`audit_events` additionally has grants `SELECT, INSERT` only — **append-only enforced at the database**, not just the application. See [docs/compliance.md](compliance.md) for the HIPAA §164.312(b) crosswalk and [pkg/persistence/postgres/README.md](../pkg/persistence/postgres/README.md) for the full schema.

### Hard-delete by patient_id (GDPR Article 17)

Documented at the package level. `DeletePatient(ctx, tenantID, patientID)` cascades through `patient_records`, removes from `interaction_records` referencing the patient, and (separately, by operator SQL) the corresponding `audit_events` rows — though GDPR vs HIPAA retention tension applies, see [docs/compliance.md](compliance.md#right-to-erasure).

---

## Failure modes and recovery

| Mode | Behaviour |
|---|---|
| Agent panic | Recovered by `InMemoryBus.Subscribe` wrapper (commit `1b8ce62`). Logged + audit event. Other agents continue. |
| `HandleMessage` returns error | Logged. Audit event with `Kind=KindAgentError`. No outputs published. Case ends prematurely (supervisor doesn't get the expected message). |
| `policy.Evaluate` returns error | Treated as deny. Logged. Audit event. Agent never sees the message. |
| LLM API timeout | Fall back to deterministic rule-based agent. `[llm-trace]` records the failure. Case finalizes. |
| Audit recorder error | Logged via `env.Logf`. Message processing continues (**fail open** on audit). |
| Bus publish error | None in `InMemoryBus`. Production buses must surface errors via the `Bus` implementation. |
| Worktree / DB connection lost | `cmd/care` falls back to in-memory at startup if `BODH_DB_DSN` connection fails. |

---

## Concurrency and ordering

### Today's reality

- `InMemoryBus` dispatches each `Publish` to subscribers via async goroutines. **No FIFO guarantee** per recipient if two `Publish` calls race.
- The diagnostic flow is serialized by the supervisor's state machine, not by the bus. The supervisor explicitly waits for `questions_complete` before forwarding to `test_planner`, then `tests_complete` before `diagnostician`. The **application protocol** does the ordering.
- Care-services fan-in points (e.g. `engagement` receiving `care_plan_ready`, `engagement_request`, `tcm_tasks_created`, `refill_response` from multiple sources) process in goroutine-scheduling order. This works in the demo because the engagement agent is idempotent.

### Production options

| Option | Trade-off |
|---|---|
| **Per-recipient FIFO bus** — pin each subscriber to a single-consumer goroutine + inbox channel | Total order per recipient; head-of-line blocking if a handler is slow |
| **Causal metadata + idempotent handlers** — every message carries `trace_id`, `causal_parents[]`, `sequence_number` | Heavier per-message; no HoL blocking; survives bus retries |

Bodh's `Message` shape supports either. Pick based on whether your bus is the bottleneck or your handlers are.

### Policy state

Policies are deliberately **stateless** today — predictable cost, easy to audit. Planned policies that need state (RateLimit, BreakGlass, Part2Consent) will take a windowed cache the orchestrator pre-binds via `ctx.Value` — never the live audit log. See [governance.md](governance.md#stateful-policies).

---

## Design rules from MARA

1. **Orchestrator coordinates; agents do not call each other.** Use `Message.To`.
2. **One agent = one meaningful clinical capability**, not one FHIR endpoint.
3. **Registry for discovery; bus for delivery.** Do not mix responsibilities.
4. **Governance runs before `HandleMessage`.** Fail closed.
5. **Prefer message-driven flows** for multi-step diagnosis; supervisor agent plans steps.
6. **Version agents and prompts**; rollback when eval metrics drop.
7. **Clinical safety:** outputs are decision **support**, not autonomous care; human attestation required for production.
8. **Cost-aware:** explicit `max_cost_usd`; avoid defaulting to "order everything."
9. **PHI hygiene:** opaque `case_id`; never log full narrative in production info-level logs.
10. **Standards-first:** prefer FHIR / HL7 / SNOMED / LOINC over custom shapes when integrating externally.

---

## Adding a new agent

See [CONTRIBUTING.md](../CONTRIBUTING.md#adding-an-agent) for the full walkthrough. Summary:

1. Create `agents/<name>.go` implementing `agent.Agent`.
2. Define `Capabilities()` honestly — what does this agent actually do?
3. Decide message types: what does it accept (input `Type`)? what does it emit (output `Type`)?
4. Write a table-driven test in `agents/<name>_test.go`.
5. Register in `cmd/demo` / `cmd/care` / `cmd/caredemo` as appropriate.
6. Update the supervisor (if it routes by type) to delegate the new message types.
7. Update [agents.md](agents.md) with the agent's contract.

The 30-line orchestrator closure handles routing, governance, audit, and observability for free.

## Adding a new policy

See [governance.md](governance.md#adding-a-policy) for the full walkthrough.

## Adding a new condition

See [data-model.md](data-model.md#adding-a-condition) and the asthma PR (#6) as a worked example. Summary:
- Add to `medical.BuildCarePlan` switch.
- Add panel rules to `DefaultPanelRules`.
- Add to `test_planner`'s `conditionWorkups` map.
- Add specialty inference to `diagnostician.inferDiagnosis`.
- Add a fixture under `data/cases/` for the bench.

**Zero platform/orchestrator/agent-framework changes required.** That's the composition pattern working.
