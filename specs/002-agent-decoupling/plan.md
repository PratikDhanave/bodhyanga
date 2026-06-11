# Bodh Agent Decoupling — Pure 3-Tier Architecture

**Status**: Plan only — awaiting user approval before any implementation  
**Reference**: Google Cloud Agent Platform, Microsoft AI Foundry  
**Reference codebase**: `github.com/PratikDhanave/PlatfromGRC` (branch `develop-towardsproduction`)

**Architectural decisions (user-approved):**
- Per-agent binary model (each agent gets its own Go binary)
- Separate Go module (`agents/` becomes `github.com/PratikDhanave/bodh/agents` with its own `go.mod`)
- Supervisor is an agent container (not backend logic)
- Zero agent code in the backend (`cmd/api/`)

---

## 0. Analysis — Evidence from the Codebase

### 0.1 The Current Monolith

`cmd/care/main.go` imports both `agents` and `pkg/agent`, wires 20+ agents in ONE process:

```go
// cmd/care/main.go imports:
"github.com/PratikDhanave/bodh/agents"       // ← agent implementations
"github.com/PratikDhanave/bodh/pkg/agent"    // ← agent interface + Message type
"github.com/PratikDhanave/bodh/pkg/orchestration" // ← orchestrator
// ... plus hitl, tasks, medical, governance, llm, knowledge

for _, a := range []agent.Agent{
    &agents.IntakeAgent{},
    &agents.DiagnosticSupervisor{},
    &agents.CDMPlannerAgent{Registry: patients},
    diag, // LLM diagnostician
    // ... 16 more
} {
    _ = reg.Register(ctx, a)
}
orch := orchestration.NewOrchestrator(reg, bus, policy, env)
orch.Start(ctx)
```

In decoupled mode, `cmd/api/` replaces `cmd/care/` as the backend. It imports NONE of the above agent-related packages. It only needs `pkg/protocol`, `pkg/comm`, and infrastructure packages.

### 0.2 Agent Dependency Matrix (Complete Audit — 38 structs, 29 bus IDs)

**Category A — Pure agents (zero external state, trivially deployable)**

| Agent ID | Struct | External deps | Notes |
|----------|--------|--------------|-------|
| intake | IntakeAgent{} | none | zero-value |
| diagnostic_supervisor | DiagnosticSupervisor{} | none | zero-value, state machine |
| questioner | QuestionerAgent{} | none | zero-value |
| test_planner | TestPlannerAgent{} | none | zero-value |
| cost_guardian | CostGuardianAgent{} | none | zero-value |
| diagnostician | DiagnosticianAgent{} | none | rule-based, zero-value |
| reasoning_verifier | ReasoningVerifierAgent{} | none | HITL flag only |
| cardiologist | CardiologistAgent{} | none | zero-value |
| pharmacist | PharmacistAgent{} | none | zero-value |
| neurologist | NeurologistAgent{} | none | zero-value |
| nephrologist | NephrologistAgent{} | none | zero-value |
| oncologist | OncologistAgent{} | none | zero-value |
| ophthalmologist | OphthalmologistAgent{} | none | zero-value |
| pulmonologist | PulmonologistAgent{} | none | zero-value |
| infectious_disease | InfectiousDiseaseAgent{} | none | zero-value |
| radiologist | RadiologistAgent{} | none | zero-value, NOT in cmd/care yet |
| rpm_monitor | RPMMonitorAgent{} | none | zero-value |
| trial_screener | ClinicalTrialScreenerAgent{} | none | zero-value |

18 agents — nearly half — need NOTHING except the message bus.

**Category B — LLM agents (need llm.Client, possibly knowledge.Store)**

| Agent ID | Variant struct | External deps |
|----------|---------------|--------------|
| diagnostician | LLMDiagnostician | llm.Client, knowledge.Store |
| diagnostician | DebateDiagnostician | llm.Client, knowledge.Store |
| diagnostician | ReflexionDiagnostician | llm.Client |
| diagnostician | CascadeDiagnostician | llm.Client |
| diagnostician | LLMDiagnosticianVision | llm.Client |
| diagnostician | ToolUsingDiagnostician | llm.Client, tools.Registry |
| questioner | LLMQuestioner | llm.Client |
| reasoning_verifier | LLMReasoningVerifier | llm.Client |
| inbox_triage | LLMInboxTriage | llm.Client, **TaskStore** |

9 LLM variants. Note: all 6 diagnostician variants share the bus ID `diagnostician` — one binary with a `--provider` flag selects which variant to run (same as today's `cmd/care -diagnostician=llm-anthropic`).

**Category C — State-dependent agents (need shared data stores)**

| Agent ID | Struct | State deps | Current type | Method calls used |
|----------|--------|-----------|-------------|------------------|
| cdm_planner | CDMPlannerAgent | PatientRegistry | `*medical.PatientRegistry` | Get, Upsert |
| tcm_coordinator | TCMCoordinatorAgent | PatientRegistry + TaskQueue | concrete structs | Get, Upsert, Create |
| panel_manager | PanelManagerAgent | PatientRegistry | `*medical.PatientRegistry` | Get, Upsert, List |
| pre_visit_prep | PreVisitPrepAgent | PatientRegistry | `*medical.PatientRegistry` | Get |
| readmission_tracker | ReadmissionTrackerAgent | PatientRegistry | `*medical.PatientRegistry` | Get, Upsert |
| refill_manager | RefillManagerAgent | PatientRegistry + TaskQueue | concrete structs | Get, Upsert, Create |
| inbox_triage | InboxTriageAgent | TaskQueue | `*tasks.Queue` | Create |
| prior_auth | PriorAuthAgent | TaskQueue | `*tasks.Queue` | Create |
| human_review | HumanReviewAgent | ReviewQueue | `*hitl.Queue` | Submit, Decide |
| engagement | EngagementAgent | Notifier | `engagement.Notifier` | ✅ already interface |
| audio_transcriber | AudioTranscriberAgent | Transcriber, Recorder | interfaces | ✅ already interfaces |

11 agents need shared state. engagement and audio_transcriber already use interfaces.

### 0.3 The Shared State Problem

Currently shared state is in-memory:
```go
patients := medical.NewPatientRegistry()   // *medical.PatientRegistry
queue    := tasks.NewQueue()               // *tasks.Queue
reviews  := hitl.NewQueue()               // *hitl.Queue
```

Three concrete types. Agents reference them directly. In separate containers, this breaks — there is no shared memory.

**Existing Postgres adapter**: `pkg/persistence/postgres/PatientRegistryAdapter` already satisfies `PatientRegistry`'s method surface (Upsert, Get, List, FilterByCondition). It exists but agents can't use it because the field type is the concrete `*medical.PatientRegistry`.

**No Postgres adapters yet**: `tasks.Queue` and `hitl.Queue` have no Postgres backing.

**Exact method surfaces needed per interface:**

```go
// TaskStore — only Create is called by agents (verified from source)
type TaskStore interface {
    Create(t NurseTask) NurseTask
    List() []NurseTask           // called by cmd/api HTTP handler
}
// *tasks.Queue satisfies this already

// ReviewStore — Submit + Decide by agent; Get + List + Pending by cmd/api
type ReviewStore interface {
    Submit(r medical.ReviewRequest) medical.ReviewRequest
    Decide(id, reviewerID, decision, reason, modification string) (medical.ReviewRequest, bool)
    Get(id string) (medical.ReviewRequest, bool)
    List() []medical.ReviewRequest
    Pending() []medical.ReviewRequest
}
// *hitl.Queue satisfies this already

// PatientStore — already documented
type PatientStore interface {
    Get(id string) (PatientRecord, bool)
    Upsert(p PatientRecord)
    List() []PatientRecord
    FilterByCondition(code string) []PatientRecord
}
// *medical.PatientRegistry satisfies this already
// *postgres.PatientRegistryAdapter satisfies this already
```

### 0.4 The HITL Resume Problem (Critical — Missed in Earlier Plans)

Today `cmd/care/main.go` line 323 calls:
```go
req, resume, ok := reviewAgent.ApplyDecision(id, reviewerID, decision, reason, modification)
for _, m := range resume { bus.Publish(r.Context(), m) }
```

`reviewAgent` is `*agents.HumanReviewAgent`. **This is an `agents` package import in the HTTP handler.** In decoupled mode, `cmd/api/` cannot import `agents`.

`ApplyDecision` does exactly two things:
1. Calls `a.Queue.Decide(...)` — pure store operation
2. Constructs a resume `protocol.Message` from `ReviewRequest.NextTo`, `NextType`, `Payload`, `Meta`

The `ReviewRequest` struct (in `pkg/medical/review.go`) already has all fields needed:
```go
type ReviewRequest struct {
    NextTo   string         // agent to forward to on approval
    NextType string         // message type to use on resume
    Payload  string         // body to forward on approval
    Meta     map[string]any // correlation metadata
    ...
}
```

**Solution**: The resume logic (~30 lines) must be extracted from `agents/human_review.go` into `pkg/hitl/` as a standalone helper function. The backend calls this helper + `ReviewStore.Decide()` + `bus.Publish()`. No agent import required.

```go
// pkg/hitl/resume.go (NEW) — extracted from agents/human_review.go
func BuildResumeMessage(r medical.ReviewRequest, reviewerID, modification string) (protocol.Message, bool) {
    payload := r.Payload
    if r.Status == medical.ReviewModified && modification != "" {
        payload = modification
    }
    meta := copyMeta(r.Meta)
    meta["hitl_review_id"] = r.ID
    meta["hitl_reviewer_id"] = reviewerID
    delete(meta, "hitl_kind")
    return protocol.NewMessage("human_review", r.NextTo, protocol.RoleAgent, r.NextType, payload, meta), true
}
```

### 0.5 The Orchestrator Analysis

`pkg/orchestration/orchestrator.go` does five things in its 30-line closure (line 244):

1. **Governance**: `o.policy.Evaluate()` — in decoupled mode, each `pkg/runtime` gateway pipeline
2. **Audit**: `o.recordAudit()` — in decoupled mode, each `pkg/runtime` audit middleware  
3. **Agent execution**: `handler.HandleMessage()` — each `pkg/runtime` message loop
4. **Eval mirror** (line 340): hardcoded check for supervisor terminal diagnosis — becomes standalone NATS subscriber
5. **Fan-out**: `bus.Publish()` — each `pkg/runtime` message loop

**The orchestrator does NOT disappear.** `cmd/care/` (monolith dev mode) continues to use it exactly as today. It is not touched. Only the new decoupled mode skips it.

### 0.6 NATS JetStream — Already Production-Ready

`pkg/comm/nats.go` (355 lines) already has:
- Durable consumers per agent (`consumerName(agentID)` function)
- `AckExplicit` — messages survive agent container restarts
- Subject routing: `bodh.{tenant}.{agentID}`
- Broadcast subject: `bodh.{tenant}.broadcast`
- JSON serialization of `protocol.Message`

No NATSBus code changes needed. Each agent binary calls `comm.NewNATSBus()` independently. The existing code already supports this.

### 0.7 The Wire Type: pkg/protocol, NOT pkg/agent

`pkg/agent/types.go` defines:
```go
type Message = protocol.Message   // type alias
```

**The backend (`cmd/api/`) uses `protocol.Message` directly**, never `agent.Message` (same type, different import path). This is the key constraint: `cmd/api/` must NOT import `pkg/agent` even though the types are identical.

---

## 1. The Problem

`cmd/care/main.go` is a monolith. 20+ agents, one process, one container, one module boundary.

- Diagnostician update forces redeploying questioner
- LLM agents starve rule-based agents for CPU
- One agent panic can kill the process
- All agents share the same API keys
- Scaling means scaling everything
- Cannot run agents in different clouds, regions, or on GPUs
- Backend is coupled to agent implementations by import

---

## 2. Target Architecture

```
┌──────────────┐   HTTP    ┌─────────────────────┐   NATS   ┌──────────────────────────┐
│  Frontend    │──────────▶│   API Service        │─────────▶│  NATS JetStream          │
│  (React SPA) │           │   cmd/api/           │          │                          │
│  Static/CDN  │           │                      │          │  bodh.{t}.intake         │
│              │           │  - HTTP endpoints    │          │  bodh.{t}.supervisor     │
│              │           │  - Auth (JWT+OPA)    │          │  bodh.{t}.diagnostician  │
│              │           │  - Postgres reads    │          │  bodh.{t}.questioner     │
│              │           │  - HITL management   │          │  bodh.{t}.human_review   │
│              │           │  - Publish to NATS   │          │  bodh.{t}.broadcast      │
│              │           │                      │          │  ... 29 subjects total   │
│              │           │  ❌ NO agents/        │          │                          │
│              │           │  ❌ NO pkg/agent       │          └──────────────────────────┘
│              │           │  ❌ NO orchestration  │
└──────────────┘           └─────────────────────┘
                                                         Each agent = own binary, own process:
                                                         ┌──────────┐  ┌──────────────┐
                                                         │  intake  │  │  supervisor  │
                                                         │  binary  │  │  binary      │
                                                         └──────────┘  └──────────────┘
                                                         ┌──────────┐  ┌──────────────┐
                                                         │  dx-llm  │  │  cdm-planner │
                                                         │  binary  │  │  binary      │
                                                         │ (+ HPA)  │  │ (+ Postgres) │
                                                         └──────────┘  └──────────────┘
```

---

## 3. Module Architecture (User Decision: Separate Go Module)

### Two modules, one workspace

```
github.com/PratikDhanave/bodh          ← MAIN MODULE (SDK + backend)
  go.mod                               ← module github.com/PratikDhanave/bodh
  pkg/                                 ← all shared library packages
    protocol/   — wire type (Message)  ← used by BOTH modules
    comm/       — Bus interface + NATS ← used by both
    agent/      — Agent, Environment   ← used by agents module only
    medical/    — CaseState, PatientStore (NEW interface)
    audit/      — Recorder
    governance/ — Policy
    hitl/       — ReviewStore (NEW interface) + BuildResumeMessage (NEW)
    tasks/      — TaskStore (NEW interface)
    persistence/postgres/ — all adapters
    runtime/    — agent message loop   ← NEW — used by agents module
    gateway/    — pipeline builder     ← NEW — used by agents module
    graduation/ — stage lifecycle      ← NEW
    ...rest of pkg/
  cmd/
    api/        ← NEW backend binary (zero agent imports)
    bench/      ← unchanged
    eval-mirror/ ← NEW standalone eval mirror service

github.com/PratikDhanave/bodh/agents   ← AGENTS MODULE (separate go.mod)
  agents/go.mod                        ← module github.com/PratikDhanave/bodh/agents
                                       ← requires github.com/PratikDhanave/bodh
  agents/                              ← all agent implementations (unchanged location)
    intake.go
    supervisor.go
    cdm_planner.go
    ...
  cmd/
    agents/                            ← per-agent binaries (NEW)
      intake/main.go                   ← only imports agents.IntakeAgent + pkg/runtime
      supervisor/main.go
      questioner/main.go
      test-planner/main.go
      cost-guardian/main.go
      diagnostician/main.go            ← --provider flag selects variant
      reasoning-verifier/main.go
      cdm-planner/main.go
      tcm-coordinator/main.go
      panel-manager/main.go
      pre-visit-prep/main.go
      inbox-triage/main.go
      refill-manager/main.go
      prior-auth/main.go
      human-review/main.go
      engagement/main.go
      audio-transcriber/main.go
      rpm-monitor/main.go
      trial-screener/main.go
      specialists/main.go              ← --specialty flag: cardiologist/pharmacist/etc
    care/                              ← MONOLITH DEV BINARY (moved into agents module)
      main.go                          ← unchanged code, different module
      web/                             ← React SPA (unchanged)

go.work                                ← workspace file at repo root
  go 1.26
  use .
  use ./agents
```

**Why `cmd/care/` moves into the agents module:**

`cmd/care/main.go` imports both `agents` (agent implementations) and many `pkg/` packages. If it stayed in the main module, the main module would need to `require bodh/agents`, but `bodh/agents` already `require bodh` → circular module dependency. Moving `cmd/care/` into the agents module breaks the cycle. The dependency graph becomes strictly one-way:

```
bodh/agents → bodh     ✅ one-way
bodh ←→ bodh/agents    ❌ circular (not allowed)
```

The `go.work` file at repo root ties both modules together for local development. `make test` runs in workspace mode. `go build ./...` still works from either module root.

**Binary count**: 20 per-agent binaries (specialists grouped into one), 1 API binary, 1 eval-mirror binary, 1 monolith binary = **23 binaries total**.

---

## 4. Per-Agent Binary Model

### Pattern: each binary is ~20 lines of main()

```go
// cmd/agents/intake/main.go — 20 lines
package main

import (
    "github.com/PratikDhanave/bodh/agents"
    "github.com/PratikDhanave/bodh/pkg/runtime"
)

func main() {
    runtime.Run(&agents.IntakeAgent{})
}
```

```go
// cmd/agents/cdm-planner/main.go — state-dependent agent
package main

import (
    "github.com/PratikDhanave/bodh/agents"
    "github.com/PratikDhanave/bodh/pkg/runtime"
    pgstore "github.com/PratikDhanave/bodh/pkg/persistence/postgres"
)

func main() {
    cfg := runtime.ParseConfig()
    db := pgstore.Connect(cfg.DatabaseURL)
    store := pgstore.NewPatientRegistryAdapter(db, cfg.TenantID)
    runtime.Run(&agents.CDMPlannerAgent{Registry: store})
}
```

```go
// cmd/agents/diagnostician/main.go — LLM agent with --provider flag
package main

import (
    "github.com/PratikDhanave/bodh/agents"
    "github.com/PratikDhanave/bodh/pkg/runtime"
)

func main() {
    cfg := runtime.ParseConfig()
    embedder, _ := agents.DefaultEmbedderForProvider(cfg.Provider)
    knowledgeStore, _ := agents.LoadDefaultKnowledge(cfg.Context(), embedder)
    diag, _ := agents.NewDiagnosticianWithKnowledge(cfg.Provider, knowledgeStore)
    runtime.Run(diag)
}
```

```go
// cmd/agents/specialists/main.go — all 8 specialists, --specialty flag
package main

import (
    "github.com/PratikDhanave/bodh/agents"
    "github.com/PratikDhanave/bodh/pkg/runtime"
)

func main() {
    cfg := runtime.ParseConfig()
    specialist := resolveSpecialist(cfg.Specialty) // switch on --specialty flag
    runtime.Run(specialist)
}
```

### What each binary gets for free from pkg/runtime

`runtime.Run(agent)` is the shared message loop (~100 lines) that:
1. Reads config from env vars + flags (`NATS_URL`, `DATABASE_URL`, `BODH_TENANT_ID`)
2. Connects to NATS JetStream
3. Subscribes on `bodh.{tenant}.{agent.ID()}`
4. Builds gateway pipeline from env/flags
5. For each delivered message: pre-process → HandleMessage → publish outputs → ack
6. Handles graceful shutdown on SIGTERM
7. Starts health/readiness HTTP probe on `:8080`

No manifest YAML needed. Configuration comes entirely from environment variables. This is simpler than the generic runtime model and consistent with the 12-factor app pattern.

### Contrast with rejected alternative (generic runtime)

The rejected "generic runtime + manifest" model compiles ALL agents into ONE binary and uses a YAML manifest to select which agent runs. This has a critical flaw: a security compromise of the diagnostician binary exposes intake, supervisor, questioner, and every other agent's code. The per-agent binary model provides true binary isolation — the intake binary literally does not contain the diagnostician's LLM code or API key handling.

---

## 5. pkg/runtime — The Shared Message Loop

This is the NEW package that replaces the orchestrator's 30-line closure for decoupled mode.

```go
// pkg/runtime/runtime.go
package runtime

type Options struct {
    NATSUrl     string   // from NATS_URL env
    TenantID    string   // from BODH_TENANT_ID env
    HealthAddr  string   // from RUNTIME_HEALTH_ADDR env, default :8080
    AuditLevel  string   // from RUNTIME_AUDIT_LEVEL env, default "minimal"
    // ... other options from env
}

// Run connects to NATS, subscribes the agent, and blocks until shutdown.
// This is all per-agent binaries call.
func Run(a agent.Agent, opts ...Option) {
    cfg := ParseConfig(opts...)
    bus, _ := comm.NewNATSBus(cfg.NATSUrl, cfg.TenantID)
    pipeline := gateway.BuildFromEnv(cfg)   // governance + audit middleware
    
    bus.Subscribe(a.ID(), func(ctx context.Context, msg protocol.Message) {
        // Step 1: Pre-pipeline (governance, audit, safety)
        msg, allow := pipeline.Pre(ctx, msg)
        if !allow { return }                // denied — already audited
        
        // Step 2: Agent execution
        env := &SimpleEnvironment{...}
        out, err := a.HandleMessage(ctx, msg, env)
        if err != nil {
            pipeline.RecordError(ctx, a.ID(), msg, err)
            return  // nak → redelivery
        }
        
        // Step 3: Post-pipeline (output validation)
        pipeline.Post(ctx, out)
        
        // Step 4: Fan-out
        for _, m := range out { bus.Publish(ctx, m) }
        
        // Step 5: Ack (AFTER all work succeeds)
    })
    
    // Health probe + graceful shutdown
    serveHealth(cfg.HealthAddr)
    awaitShutdown()
}
```

---

## 6. Gateway Pipeline — pkg/gateway/

Mirrors PlatformGRC's `create_gateway_pipeline()` pattern.

### Middleware interface

```go
type Middleware interface {
    Process(ctx context.Context, msg protocol.Message) (protocol.Message, bool, error)
    // bool = allow (true) or deny (false)
}
```

### Available middleware (wraps existing pkg/)

| Middleware | Wraps | Existing code |
|-----------|-------|--------------|
| audit | `pkg/audit` | wraps existing Recorder |
| policy_gate | `pkg/governance` | wraps CompositePolicy |
| phi_guard | `pkg/phi` | wraps existing |
| circuit_breaker | — | new (~80 lines) |
| content_safety | — | new (~60 lines) |
| output_validator | — | new (~60 lines) |

### Platform guarantees (always enforced by pkg/runtime, non-negotiable)

1. **Audit(minimal)** — every message, every agent. Cannot be disabled.
2. **Message timeout** — 60s default. Stuck agents do not block NATS consumers.
3. **Panic recovery** — `safeHandle` pattern from `pkg/comm/bus.go`.
4. **PHI logging rule** — `sha256(caseID)[:8]` in all spans, enforced by the runtime's logger wrapper.

Configuration is by environment variable:
```
RUNTIME_AUDIT_LEVEL=hipaa_full
RUNTIME_POLICY_MAX_CONTENT=65536
RUNTIME_CIRCUIT_BREAKER=true
```

---

## 7. API Service — cmd/api/

This is the backend binary extracted from `cmd/care/main.go`. It must import **zero** agent-related packages.

### Allowed imports in cmd/api/

```go
import (
    // Wire type only — NOT pkg/agent
    "github.com/PratikDhanave/bodh/pkg/protocol"
    
    // Message bus — publish to agents
    "github.com/PratikDhanave/bodh/pkg/comm"
    
    // Infrastructure
    "github.com/PratikDhanave/bodh/pkg/medical"     // domain types (CaseState, ReviewRequest etc)
    "github.com/PratikDhanave/bodh/pkg/hitl"         // ReviewStore interface + BuildResumeMessage
    "github.com/PratikDhanave/bodh/pkg/tasks"         // TaskStore interface
    "github.com/PratikDhanave/bodh/pkg/audit"         // Recorder
    "github.com/PratikDhanave/bodh/pkg/eval"          // Store
    "github.com/PratikDhanave/bodh/pkg/auth"          // JWT + OPA
    "github.com/PratikDhanave/bodh/pkg/governance"    // AgentMeshGate (kill switch)
    pgstore "github.com/PratikDhanave/bodh/pkg/persistence/postgres"
    // ... other pkg/ packages
    
    // ❌ NEVER:
    // "github.com/PratikDhanave/bodh/agents"
    // "github.com/PratikDhanave/bodh/pkg/agent"
    // "github.com/PratikDhanave/bodh/pkg/orchestration"
)
```

### Case submission (entry point to the pipeline)

```go
// POST /api/cases
msg := protocol.NewMessage("api", "intake", protocol.RoleUser,
    "presentation", presentationJSON, map[string]any{
        "case_id":   caseID,
        "tenant_id": tenantID,
    })
bus.Publish(r.Context(), msg)
```

### HITL review decision (no agent import needed)

```go
// POST /api/reviews/{id}/decide
var body struct { ReviewerID, Decision, Reason, Modification string }
json.Decode(r.Body, &body)

r, ok := reviewStore.Decide(id, body.ReviewerID, body.Decision, body.Reason, body.Modification)
if !ok { ... }

if r.Status == medical.ReviewApproved || r.Status == medical.ReviewModified {
    // BuildResumeMessage is in pkg/hitl — NO agents import needed
    msg, _ := hitl.BuildResumeMessage(r, body.ReviewerID, body.Modification)
    bus.Publish(r.Context(), msg)
}
writeJSON(w, r)
```

### Real-time updates via broadcast subject

```go
// Subscribe to broadcast for SSE push to frontend
bus.Subscribe("broadcast", func(ctx context.Context, msg protocol.Message) {
    sseHub.Send(msg.Metadata["case_id"], msg)
})
```

### Responsibilities

| Current in cmd/care | In cmd/api | Moves to agent binaries |
|--------------------|-----------|------------------------|
| HTTP handlers | ✅ | |
| Vue SPA serving | ✅ | |
| Auth (JWT + OPA) | ✅ | |
| Postgres persistence | ✅ | |
| HITL review management | ✅ | |
| Case submission → NATS | ✅ | |
| SSE real-time updates | ✅ | |
| Agent registration | ❌ eliminated | |
| Orchestrator | ❌ eliminated | |
| Governance policy | | ✅ per-agent in pkg/runtime |
| Agent construction | | ✅ per-agent binary main() |
| HandleMessage | | ✅ per-agent binary |

---

## 8. The Orchestrator — What Happens to It

`pkg/orchestration/orchestrator.go` is **NOT modified or deleted**.

- `cmd/care/` (monolith dev mode) continues to use it exactly as today
- All tests continue to use it with InMemoryBus
- `cmd/demo/` unchanged

In decoupled mode, its responsibilities are distributed:

| Orchestrator responsibility | Where it goes in decoupled mode |
|---------------------------|--------------------------------|
| Subscribe agents | Each per-agent binary's `runtime.Run()` |
| Evaluate governance | Each `pkg/runtime` gateway pipeline |
| Record audit | Each `pkg/runtime` audit middleware |
| Eval mirror (line 340) | `cmd/eval-mirror/` standalone NATS subscriber |
| Fan-out responses | Each `pkg/runtime` publishes to NATS |

### Eval mirror service (cmd/eval-mirror/)

Tiny standalone binary (~80 lines):
- Subscribes to `bodh.{tenant}.diagnostic_supervisor`
- Watches for terminal `diagnosis` messages
- Calls `eval.ScoreCase()` and saves to `eval.Store` (Postgres)
- Keeps DiagnosticSupervisor completely clean (zero eval dependency)

---

## 9. Communication Flow (End to End)

```
User → React SPA → POST /api/cases → cmd/api
  │
  └─ protocol.NewMessage("api", "intake", ...) → NATS bodh.default.intake
       │
   [intake binary]  IntakeAgent.HandleMessage
       └─ → bodh.default.rpm_monitor
            │
        [rpm-monitor binary]
            └─ → bodh.default.diagnostic_supervisor
                 │
             [supervisor binary]  switch msg.Type
                 └─ "case_state" → bodh.default.questioner
                      │
                  [questioner binary]
                      └─ → bodh.default.diagnostic_supervisor
                           │
                       [supervisor binary]
                           └─ "questions_complete" → bodh.default.test_planner
                                │
                            [test-planner binary]
                                └─ → bodh.default.cost_guardian
                                     │
                                 [cost-guardian binary]
                                     └─ → bodh.default.diagnostic_supervisor
                                          │
                                      [supervisor binary]
                                          └─ "tests_complete" → bodh.default.diagnostician
                                               │
                                           [diagnostician binary]  LLM call
                                               └─ → bodh.default.reasoning_verifier
                                                    │
                                                [verifier binary]
                                                    ├─ HITL: → bodh.default.human_review
                                                    │         [clinician] → POST /api/reviews/{id}/decide
                                                    │         cmd/api: ReviewStore.Decide() + hitl.BuildResumeMessage()
                                                    │         → NATS bodh.default.diagnostic_supervisor
                                                    │
                                                    └─ pass: → bodh.default.diagnostic_supervisor
                                                               "diagnosis" message (terminal)
                                                               │
                                                           [supervisor binary]  finalize, no output
                                                           [eval-mirror service] ScoreCase → Postgres

cmd/api (broadcast subscriber) ← every message mirrored
  → SSE push → React SPA → user sees diagnosis
```

---

## 10. Container Strategy

### Per-agent Docker image (simple multi-stage)

```dockerfile
# zarf/docker/Dockerfile.intake
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.work go.work.sum ./
COPY go.mod go.sum ./
COPY agents/go.mod agents/go.sum ./agents/
RUN go work sync
COPY . .
RUN CGO_ENABLED=0 go build -o /intake ./agents/cmd/agents/intake

FROM gcr.io/distroless/static-debian12
COPY --from=builder /intake /intake
ENTRYPOINT ["/intake"]
```

Each agent has its own Dockerfile. The builder stage is identical — only the final `COPY` and `ENTRYPOINT` line differs. Docker layer caching means the builder layer is shared across all 23 images.

### Per-agent Kubernetes deployment

```yaml
# zarf/k8s/agents/intake/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-intake
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: agent
          image: bodh/agent-intake:v1
          env:
            - name: NATS_URL
              value: nats://nats:4222
            - name: BODH_TENANT_ID
              value: default
          # NO DATABASE_URL — pure agent, no shared state
          # NO ANTHROPIC_API_KEY — rule-based agent
          resources:
            requests: { cpu: 50m, memory: 64Mi }
            limits:   { cpu: 100m, memory: 128Mi }
```

```yaml
# zarf/k8s/agents/diagnostician/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-diagnostician
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: agent
          image: bodh/agent-diagnostician:v1
          env:
            - name: DIAGNOSTICIAN_PROVIDER
              value: llm-anthropic
            - name: ANTHROPIC_API_KEY
              valueFrom:
                secretKeyRef:
                  name: agent-diagnostician-secrets  # THIS agent's secret only
                  key: api-key
          resources:
            requests: { cpu: 500m, memory: 512Mi }
            limits:   { cpu: "2", memory: 2Gi }
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: agent-diagnostician
spec:
  minReplicas: 1
  maxReplicas: 10
  metrics:
    - type: Resource
      resource: { name: cpu, target: { type: Utilization, averageUtilization: 70 } }
```

---

## 11. Security Model

### Per-agent secret isolation

| Agent binary | Secrets in env | Other agents see? |
|-------------|----------------|-------------------|
| diagnostician | ANTHROPIC_API_KEY | ❌ No |
| questioner-llm | OLLAMA_HOST | ❌ No |
| intake | (none) | N/A |
| cdm-planner | DATABASE_URL | ❌ No (separate secret) |
| diagnostician-debate | ANTHROPIC_API_KEY, OPENAI_API_KEY | ❌ No |

A compromised intake binary cannot read the diagnostician's API key. True environment isolation per agent — the Google Agent Platform model.

### Network policy (per-agent)

```yaml
# Only what this agent needs
egress:
  - to: [nats]          # always
  - to: [postgres]      # state-dependent agents only
  - to: []              # LLM agents only — outbound :443 for LLM API
```

---

## 12. HITL Flow in Decoupled Mode

```
1. Agent (e.g. cdm_planner) → NATS bodh.{t}.human_review
2. [human-review binary]: HumanReviewAgent.HandleMessage → ReviewStore.Submit() → Postgres
3. cmd/api: broadcast subscriber sees review_request message → SSE to frontend
4. Clinician sees review in frontend UI
5. Clinician clicks Approve → POST /api/reviews/{id}/decide → cmd/api
6. cmd/api:
   a. reviewStore.Decide(id, reviewerID, "approve", reason, "")
   b. hitl.BuildResumeMessage(r, reviewerID, "") → protocol.Message
   c. bus.Publish(ctx, msg)  ← publishes to bodh.{t}.{r.NextTo}
7. Pipeline resumes at the agent that originally requested review
```

Both `cmd/api/` and `[human-review binary]` share the same Postgres `review_requests` table via the same `ReviewStoreAdapter`. Shared state through shared database, not shared memory.

---

## 13. Error Recovery

NATS JetStream provides durability that InMemoryBus cannot:

| Failure | InMemoryBus (today) | NATS JetStream (decoupled) |
|---------|--------------------|-----------------------------|
| Agent container crashes mid-message | ❌ message lost | ✅ redelivered after AckWait timeout (30s) |
| Agent returns error | ❌ message lost | ✅ nak → immediate redelivery |
| Permanent failure (N retries) | ❌ message lost | ✅ DLQ for inspection |
| New agent version rollout | ❌ messages in flight lost | ✅ durable consumer persists messages |

Ack happens AFTER all outputs are published. If the agent binary crashes after HandleMessage but before all outputs are published, NATS redelivers — the agent must be idempotent (current agents are, they compute from message content alone).

---

## 14. New Package Layout

```
[main module: github.com/PratikDhanave/bodh]
cmd/
  api/                    NEW — HTTP API service (zero agent imports)
    main.go
  eval-mirror/            NEW — NATS→eval.Store subscriber
    main.go
  bench/                  UNCHANGED
  bodhadmin/              UNCHANGED
  bodhctl/                UNCHANGED

pkg/
  protocol/               UNCHANGED — wire type (Message)
  comm/                   UNCHANGED — Bus interface + NATSBus
  agent/                  UNCHANGED — Agent, Environment interfaces
  medical/
    store.go              NEW — PatientStore interface
    (all other files)     UNCHANGED
  tasks/
    store.go              NEW — TaskStore interface
    (all other files)     UNCHANGED
  hitl/
    store.go              NEW — ReviewStore interface
    resume.go             NEW — BuildResumeMessage() extracted from agents
    (all other files)     UNCHANGED
  runtime/                NEW — shared agent message loop
    runtime.go            Run(), ParseConfig(), SimpleEnvironment
    health.go             health/readiness HTTP probes
  gateway/                NEW — composable pipeline
    middleware.go         Middleware interface + Pipeline type
    builder.go            BuildFromEnv()
    middlewares/
      audit.go            wraps pkg/audit
      policy.go           wraps pkg/governance
      phi.go              wraps pkg/phi
      breaker.go          circuit breaker
      safety.go           content safety
  graduation/             NEW — agent lifecycle management
    stage.go              Stage enum (development → shadow → copilot → autopilot)
    wilson.go             Wilson score confidence interval
  persistence/postgres/
    task_adapter.go       NEW — TaskStoreAdapter (Postgres-backed tasks.Queue)
    review_adapter.go     NEW — ReviewStoreAdapter (Postgres-backed hitl.Queue)
    (existing files)      UNCHANGED
  (all other pkg/)        UNCHANGED

go.work                   NEW — workspace file
  go 1.26
  use .
  use ./agents

[agents module: github.com/PratikDhanave/bodh/agents]
agents/
  go.mod                  NEW — module github.com/PratikDhanave/bodh/agents
  go.sum                  NEW
  agents/                 UNCHANGED — all agent .go files stay here
    intake.go
    supervisor.go
    cdm_planner.go        MODIFIED — field type *medical.PatientRegistry → medical.PatientStore
    (all other agents)    MODIFIED — same interface extraction for Tasks, Queue fields
  cmd/
    agents/               NEW — per-agent binaries
      intake/main.go
      supervisor/main.go
      questioner/main.go
      test-planner/main.go
      cost-guardian/main.go
      diagnostician/main.go       --provider flag
      reasoning-verifier/main.go
      cdm-planner/main.go
      tcm-coordinator/main.go
      panel-manager/main.go
      pre-visit-prep/main.go
      inbox-triage/main.go
      refill-manager/main.go
      prior-auth/main.go
      human-review/main.go
      engagement/main.go
      audio-transcriber/main.go
      rpm-monitor/main.go
      trial-screener/main.go
      specialists/main.go         --specialty flag
    care/                         MOVED from top-level cmd/care
      main.go                     UNCHANGED code
      web/                        UNCHANGED

zarf/
  docker/
    Dockerfile.intake             NEW (one per agent, or Makefile-generated)
    Dockerfile.diagnostician      NEW
    Dockerfile.api                NEW
    Dockerfile.care               KEPT (unchanged for monolith mode)
    ... (22 total)
  k8s/
    agents/                       NEW — per-agent k8s manifests
      intake/deployment.yaml
      diagnostician/deployment.yaml
      diagnostician/hpa.yaml
      ... (one dir per agent)
    api/deployment.yaml           NEW
    nats/                         NEW
  compose/
    decoupled.yaml                NEW — local decoupled dev stack
```

---

## 15. Three Deployment Modes

| Mode | Binary | Bus | State | When |
|------|--------|-----|-------|------|
| **Monolith** | `cmd/care/` | InMemoryBus | in-memory structs | local dev, demos, CI |
| **Hybrid** | per-agent binaries, some grouped | NATS | Postgres | staging, cost-sensitive |
| **Pure** | per-agent binaries, own containers | NATS JetStream | Postgres | production |

The monolith mode (`cmd/care/`) continues to work exactly as today — zero code changes to it, zero changes to how it's deployed. It is the fast-feedback mode for development.

---

## 16. Comparison with Reference Architectures

### Google Cloud Agent Platform

| Google | Bodh |
|--------|------|
| Agent Container (Cloud Run) | `cmd/agents/{name}/` binary + K8s Pod |
| Agent Definition (ADK) | per-binary `main.go` + env vars |
| Per-agent IAM | K8s ServiceAccount + per-agent Secret |
| Cloud Run autoscaling | K8s HPA per agent |
| A2A protocol | `pkg/a2a/` (already built) |
| Vertex AI evaluation | `cmd/eval-mirror/` + `pkg/eval/` |

### Microsoft AI Foundry

| Microsoft | Bodh |
|-----------|------|
| Agent Service | `cmd/agents/{name}/` binary |
| Managed Identity | K8s ServiceAccount |
| Content Safety | `pkg/gateway/middlewares/safety/` |
| Guardrails | `pkg/gateway/` pipeline |
| Prompt Flow | MARA pipeline via supervisor |

### PlatformGRC

| PlatformGRC | Bodh |
|-------------|------|
| `loop.py` agent loop | `pkg/runtime/runtime.go` |
| `create_gateway_pipeline()` | `gateway.BuildFromEnv()` |
| `PolicyMiddleware` | `gateway/middlewares/policy.go` |
| `AuditMiddleware` | `gateway/middlewares/audit.go` |
| `PiiMiddleware` | `gateway/middlewares/phi.go` |
| `CircuitBreakerMiddleware` | `gateway/middlewares/breaker.go` |
| `ContentSafetyMiddleware` | `gateway/middlewares/safety.go` |
| `graduation.py` | `pkg/graduation/` |

---

## 17. Implementation Phases

### Phase 0: Interface Extraction (Prerequisites — no deployment change)

All changes are inside existing packages. `cmd/care/` continues to work unchanged throughout. `make test` must pass after every task.

| Task | What | File(s) | Est. LOC |
|------|------|---------|---------|
| 0.1 | Extract `PatientStore` interface | `pkg/medical/store.go` (NEW) | ~25 |
| 0.2 | Extract `TaskStore` interface | `pkg/tasks/store.go` (NEW) | ~15 |
| 0.3 | Extract `ReviewStore` interface | `pkg/hitl/store.go` (NEW) | ~20 |
| 0.4 | Extract `BuildResumeMessage()` | `pkg/hitl/resume.go` (NEW) | ~35 |
| 0.5 | Change agent struct fields to interfaces | 9 agent files | ~15 |
| 0.6 | Add `TaskStoreAdapter` | `pkg/persistence/postgres/task_adapter.go` (NEW) | ~120 |
| 0.7 | Add `ReviewStoreAdapter` | `pkg/persistence/postgres/review_adapter.go` (NEW) | ~150 |
| 0.8 | Migration SQL for tasks + reviews tables | `db/migrations/*.sql` | ~60 SQL |

**Phase 0 total: ~440 LOC. Zero behavior change. Zero deployment change.**

Validation gate: `make test` passes, `make build` passes, `cmd/care` runs identically.

### Phase 1: Core Packages (new pkg/ packages)

| Task | What | File(s) | Est. LOC |
|------|------|---------|---------|
| 1.1 | `pkg/runtime/` — agent message loop | `runtime.go`, `health.go` | ~200 |
| 1.2 | `pkg/gateway/` — pipeline builder | `middleware.go`, `builder.go`, 5 middleware files | ~350 |
| 1.3 | `pkg/graduation/` — stage lifecycle | `stage.go`, `wilson.go` | ~150 |

**Phase 1 total: ~700 LOC.**

### Phase 2: Module Split

| Task | What | Est. effort |
|------|------|------------|
| 2.1 | Create `agents/go.mod` (module `github.com/PratikDhanave/bodh/agents`) | ~5 min |
| 2.2 | Create `go.work` with `use . / use ./agents` | ~2 min |
| 2.3 | Move `cmd/care/` → `agents/cmd/care/` | ~5 min (git mv) |
| 2.4 | Verify `make test` still passes in workspace mode | validation |

**Phase 2 total: ~15 min mechanical work + test run.**

### Phase 3: Binaries

| Task | What | File(s) | Est. LOC |
|------|------|---------|---------|
| 3.1 | 20 per-agent binaries in `agents/cmd/agents/*/main.go` | 20 files × ~20 lines | ~400 |
| 3.2 | `cmd/api/main.go` — backend (zero agent imports) | 1 file | ~500 |
| 3.3 | `cmd/eval-mirror/main.go` | 1 file | ~80 |

**Phase 3 total: ~980 LOC.**

### Phase 4: Containers & Orchestration

| Task | What | Est. LOC |
|------|------|---------|
| 4.1 | 22 Dockerfiles (or Makefile-generated pattern) | ~500 |
| 4.2 | Docker Compose for local decoupled dev | ~200 YAML |
| 4.3 | K8s manifests for all agents + API + NATS | ~1500 YAML |

**Phase 4 total: ~2200 LOC (mostly YAML).**

### Phase 5: Integration Tests

| Task | What | Est. LOC |
|------|------|---------|
| 5.1 | Multi-container integration test (testcontainers) | ~300 |
| 5.2 | HITL flow across containers | ~150 |
| 5.3 | Eval mirror end-to-end | ~100 |

**Phase 5 total: ~550 LOC.**

### Grand Total: ~5,870 LOC (Go + YAML)

Existing code changes: ~440 LOC (Phase 0 only — interface extraction + resume extraction).  
All other code is new. Zero breaking changes to `cmd/care/` or any existing functionality.

---

## 18. Open Questions

1. **NATS message size**: `CaseState` JSON is passed in every message. NATS default max is 1MB. Largest CaseState today (with test results + hypotheses) is ~5KB. No issue. If future clinical data grows (e.g. image references), consider a state-pointer pattern where messages carry `state_id` and agents fetch from Postgres. Not needed now.

2. **Local dev experience**: 20+ containers is heavy for local dev. The three-mode model handles this: use `cmd/care/` monolith for daily development. Only bring up decoupled stack for integration testing.

3. **Multi-tenant isolation**: Share one agent binary per agent across all tenants (NATS subject routing `bodh.{tenant}.*` + Postgres RLS isolates data). Do NOT run one binary per tenant per agent — that's O(tenants × agents) containers. Current NATS + RLS design already handles this correctly.

4. **A2A external exposure**: Each per-agent binary could expose A2A HTTP endpoint for cross-platform interop. `pkg/a2a/` is already built. Add as Phase 2 enhancement to `pkg/runtime/runtime.go`.

5. **Radiologist agent**: `RadiologistAgent` is defined but NOT registered in `cmd/care/main.go`. It gets a binary (`cmd/agents/radiologist/`) but its omission from cmd/care may be intentional. Flag for confirmation before wiring.

6. **Diagnostician factory calls in cmd/care**: `agents.NewDiagnosticianWithKnowledge`, `agents.DefaultEmbedderForProvider`, `agents.LoadDefaultKnowledge` are currently called from `cmd/care/main.go`. When `cmd/care/` moves into the agents module, these stay exactly where they are — no change needed. When writing `cmd/agents/diagnostician/main.go`, these same functions are called from there too.
