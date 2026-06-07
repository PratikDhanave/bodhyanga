# ADR 0004: Multi-Agent Reference Architecture (MARA) Pipeline

**Status:** Accepted

**Date:** 2024-12-18

**Deciders:** Pratik Dhanave

---

## Context

Bodh is a clinical decision support platform. A single patient encounter
produces a sequence of actions that must happen in order: intake (capture
chief complaint and history), test planning (decide which labs/imaging to
order), diagnostic reasoning (differential diagnosis from available data),
and care plan generation (treatment recommendations). Each step requires
different domain knowledge, different prompt strategies, and different
structured output schemas.

A monolithic approach -- one giant prompt that performs all four steps -- has
several problems in the clinical domain:

- **Audit granularity.** HIPAA requires logging every action taken on PHI.
  A monolithic call produces one audit entry for the entire encounter; a
  pipeline produces one entry per step, each attributable to a named agent.

- **Human-in-the-loop gates.** Regulatory requirements (21 USC SS 360j(o)(1)
  carve-out criterion 4) demand that a licensed clinician independently review
  AI-generated diagnoses before clinical action. A gate must be insertable
  between the diagnostician and the care plan generator without restructuring
  the entire flow.

- **Provider flexibility per step.** Some health systems want high-accuracy
  cloud LLMs for diagnosis but local Ollama models for intake (where PHI
  is most sensitive). A pipeline allows per-step provider selection.

- **Evaluation per step.** Bench and eval harnesses need to measure diagnostic
  accuracy independently of intake quality. A pipeline exposes each step's
  input/output for isolated scoring.

Microsoft's Multi-Agent Reference Architecture (MARA) formalizes this
pattern: an orchestrator coordinates specialized agents that communicate via
a message bus. Agents do not call each other directly; they publish typed
messages that the orchestrator routes.

## Decision

Implement the MARA pipeline with four layers:

### 1. Agent abstraction (`pkg/agent`)

Every agent implements a single interface:

```go
type Agent interface {
    ID() string
    HandleMessage(ctx context.Context, msg Message, env Environment) ([]Message, error)
}
```

Agents are stateless functions: they receive one message, return zero or more
messages, and have no direct reference to other agents. The `Environment`
parameter provides logging and time; richer services (LLM client, knowledge
base) are injected via composition in the concrete agent struct.

96 agent definitions live in `agents/` as YAML and Go files, covering the
clinical pipeline plus care-services workflows (CDM, TCM, panel management,
RPM, inbox triage, refill management, prior authorization).

### 2. Message bus (`pkg/comm`)

The bus is a publish/subscribe transport with a minimal interface:

```go
type Bus interface {
    Publish(ctx context.Context, msg agent.Message)
    Subscribe(agentID string, handler func(context.Context, agent.Message))
}
```

`InMemoryBus` dispatches each published message in a new goroutine via
`safeHandle`, which recovers panics so one misbehaving agent cannot crash
the process. Production deployments swap in Kafka, NATS, or RabbitMQ behind
the same interface.

### 3. Orchestrator (`pkg/orchestration`)

The `Orchestrator` is the central coordinator. Its `Start` method snapshots
the agent registry and subscribes each agent to the bus. When a message
arrives for an agent, the orchestrator:

1. **Evaluates governance policy** via `Policy.Evaluate()`. A denied message
   never reaches the agent (fail-closed). Denies are always audited; allows
   are audited only for clinical message types to keep the log signal-rich.

2. **Records the agent invocation** in the audit log before calling
   `HandleMessage`. This ensures a panicking agent still leaves an audit trail.

3. **Delegates to the agent** via `HandleMessage`. The agent returns zero or
   more output messages.

4. **Mirrors terminal diagnoses** into the eval store for accuracy scoring.

5. **Publishes output messages** back to the bus for routing to the next agent.

The orchestrator contains zero clinical logic. It never branches on diagnosis,
condition, or test results. Adding a new agent type is zero orchestrator-side
change.

### 4. Governance (`pkg/governance`)

`CompositePolicy` composes multiple policy evaluators. The shipped policies
include:

- **RequireCaseIDPolicy:** Clinical message types (`presentation`,
  `case_state`, `ask_question`, `order_test`, `diagnosis`,
  `verify_reasoning`) must carry a `case_id` in metadata. Messages without
  one are denied, preventing untracked clinical actions.

- **KillSwitchPolicy:** Checks the `KillSwitchRegistry` for active halt
  scopes (global, per-agent, per-capability). Operators can quarantine a
  misbehaving agent without restarting the process.

- **MedicalPolicy:** Domain-specific rules (e.g., require reviewer sign-off
  for high-risk diagnoses).

### Clinical pipeline flow

```
Patient        Intake         Test           Diagnostician    Care Plan
Encounter  --> Agent      --> Planner    -->   Agent      --> Generator
               |              |                |              |
               v              v                v              v
           KindMessage    KindMessage     KindMessage     KindMessage
           (audit)        (audit)         (audit)         (audit)
                                               |
                                               v
                                          HITL Gate
                                          (KindReviewDecision)
```

Each arrow is a `Publish` on the bus. Each agent hop produces a
`KindMessage` audit event. The HITL gate between the diagnostician and
the care plan generator produces a `KindReviewDecision` event. Policy
decisions produce `KindPolicyDecision` events.

## Consequences

### Positive

- **Deterministic, auditable pipeline.** Every step is a named agent with
  a typed message. The audit log reconstructs the full reasoning chain for
  any case via `GET /explain/{case_id}`, which joins audit events by case_id
  and renders the step-by-step trace.

- **HITL gates are composable.** The `pkg/hitl` queue can be inserted between
  any two agents by having the upstream agent address its output to the gate
  agent instead of the downstream agent. The gate holds the message until a
  reviewer decides, then re-publishes to the downstream agent. No pipeline
  restructuring is needed.

- **Agents are independently testable.** Each agent is a pure function from
  `(Message, Environment)` to `([]Message, error)`. Tests construct a message,
  inject a mock LLM client via the environment, and assert on the output
  messages. No bus, no orchestrator, no other agents needed.

- **Zero orchestrator changes for new agents.** The 96 agent definitions in
  `agents/` were added without modifying `pkg/orchestration/orchestrator.go`.
  The orchestrator discovers agents via the registry at `Start` time.

- **Per-step provider selection.** Because each agent holds its own `llm.Client`
  reference, different agents can use different providers. The intake agent
  can use local Ollama while the diagnostician uses cloud Anthropic.

### Negative

- **Message serialization overhead.** Every inter-agent hop serializes and
  deserializes the clinical state as JSON in `msg.Content`. For the in-memory
  bus this is a JSON round-trip per hop (microseconds, not a practical concern).
  For a distributed bus (Kafka) it adds network serialization cost.

- **Debugging requires following the bus.** Unlike a direct function call
  chain, the data flow is mediated by the bus. Developers must understand
  the subscribe/publish model to trace a case through the system. Mitigated
  by the audit log and the `/explain/{case_id}` endpoint, which reconstruct
  the flow as a linear trace.

- **No dynamic agent addition after Start.** The orchestrator snapshots the
  registry at `Start` time via `sync.Once`. Agents registered after `Start`
  are not auto-subscribed. Hot-reload is on the roadmap but not yet
  implemented.

### Neutral

- The message bus interface is intentionally minimal (Publish + Subscribe).
  Advanced features (consumer groups, dead-letter queues, exactly-once
  delivery) are the responsibility of the concrete bus implementation, not
  the orchestrator.

- The orchestrator uses `sync.Once` to guarantee idempotent startup. Calling
  `Start` twice is a no-op, eliminating the double-subscription bug that
  previously caused duplicate message delivery.

## References

- `pkg/orchestration/orchestrator.go` -- Orchestrator, Start, governance integration
- `pkg/agent/` -- Agent interface, Message type, Environment interface
- `pkg/comm/` -- Bus interface, InMemoryBus with safeHandle panic recovery
- `pkg/governance/` -- CompositePolicy, RequireCaseIDPolicy, KillSwitchPolicy
- `pkg/hitl/` -- Human-in-the-loop review queue
- `pkg/audit/recorder.go` -- Audit event schema and Recorder interface
- `agents/` -- 96 YAML/Go agent definitions
- `cmd/care/explain.go` -- GET /explain/{case_id} audit trail reconstruction
