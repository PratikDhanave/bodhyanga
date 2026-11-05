package agent

// File types.go: the Agent and Environment interfaces plus the Message
// re-exports. See doc.go for the package-level overview.
//
// MARA mapping (see docs/architecture.md § The five core interfaces):
//
//   - Agent          — interface 1 of 5; the unit of clinical work.
//   - Environment    — interface 5 of 5; the per-call service locator.
//   - Bus            — interface 2, lives in pkg/comm.
//   - Registry       — interface 3, lives in pkg/registry.
//   - Policy         — interface 4, lives in pkg/governance.
//
// Design rule 1 from MARA: agents never call each other directly. They emit
// agent.Message values with a "To" field; the orchestrator + bus do the
// routing. Every interaction passes through the orchestrator's 30-line
// closure (see pkg/orchestration/orchestrator.go Start) where governance,
// audit, and observability are enforced — exactly once. If you find
// yourself wanting to call another agent's method directly, you're
// fighting the architecture and your change will silently bypass HIPAA
// audit, fail-closed governance, and the HITL gate.

import (
	"context"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
)

// Re-export core protocol types for convenience.
//
// Architectural rationale:
//   - The bus (pkg/comm), governance (pkg/governance), and orchestration
//     (pkg/orchestration) all need to refer to the wire format.
//   - If those packages imported pkg/agent just to name Message, we'd risk
//     dependency cycles (e.g. orchestration → agent → governance → agent).
//   - The canonical definition lives in pkg/protocol (no Go deps inside the
//     module), and pkg/agent re-exports the type because agent authors work
//     with messages on every single line and the ergonomics matter.
//
// These are Go type aliases (`type X = Y`), not new types — agent.Message
// and protocol.Message are interchangeable at the type system level; you
// can pass one where the other is expected without conversion.
type Message = protocol.Message

// MessageRole mirrors protocol.MessageRole. Used by governance policies that
// branch on origin (e.g. RequireTenantPolicy ignores RoleSystem broadcasts).
type MessageRole = protocol.MessageRole

// Role constants are re-exported so agents can write agent.RoleAgent without
// pulling in pkg/protocol. The list mirrors protocol.MessageRole exactly —
// see pkg/protocol/message.go for the per-role semantics.
//
// Governance treats these as the primary origin signal:
//   - RoleUser drives the entry of every clinical case (the initial
//     "presentation" message).
//   - RoleAgent is the in-system default for every agent-to-agent hop.
//   - RoleSystem is reserved for control-plane broadcasts and is exempt from
//     some require-* policies (see pkg/governance).
//   - RoleObserver / RoleEvaluator flag passive telemetry vs scoring events
//     so the orchestrator can fan them out to sinks without re-running
//     clinical policies.
const (
	RoleUser      = protocol.RoleUser
	RoleSystem    = protocol.RoleSystem
	RoleAgent     = protocol.RoleAgent
	RoleTool      = protocol.RoleTool
	RoleObserver  = protocol.RoleObserver
	RoleEvaluator = protocol.RoleEvaluator
)

// NewMessage constructs a new protocol message.
//
// This is a thin wrapper so agent implementations can do
// `agent.NewMessage(...)` without importing pkg/protocol directly. It also
// guarantees the message carries a fresh UUID (for audit / causal graphs)
// and a UTC CreatedAt timestamp — see protocol.NewMessage for the defaults.
//
// Convention used throughout agents/*.go: pass copyMeta(msg.Metadata) (from
// agents/supervisor.go) as the final argument so downstream agents inherit
// case_id, trace_id, tenant_id, and other correlation keys. Mutating the
// caller's metadata map by accident is a common footgun this convention
// avoids.
func NewMessage(from string, to string, role MessageRole, msgType, content string, metadata map[string]any) Message {
	return protocol.NewMessage(from, to, role, msgType, content, metadata)
}

// Agent describes the minimal interface for an agent in the system.
//
// This is the canonical Bodhyanga agent contract — four methods, no more. Every
// agent in agents/*.go (17+ today) implements this interface. The
// orchestrator's subscription closure (see
// pkg/orchestration/orchestrator.go) is built around it: it calls
// HandleMessage exactly once per delivered message, captures the returned
// slice, and re-publishes each element back through the bus.
//
// What's deliberately NOT here:
//   - No "Tools" method. Tool use is internal to an agent that wraps an
//     llm.Client with Tools support (see agents/diagnostician_tools.go).
//   - No "Memory" method. State sharing is by injected struct fields
//     (PatientRegistry, tasks.Queue, hitl.Queue) — see docs/agents.md
//     § Shared state injected at construction.
//   - No "Init" lifecycle. Construction is the constructor; teardown is the
//     bus unsubscribe function. Long-running per-agent goroutines are an
//     anti-pattern in this architecture — agents are pure
//     message → []message functions in spirit.
//
// In this reference implementation, orchestration is message-driven: agents
// do not call each other directly. They emit messages that are routed by
// the bus. This keeps coupling low, supports swapping implementations
// (Kafka, NATS, RabbitMQ for the bus; etcd, Consul for the registry), and
// makes every interaction observable, auditable, governable, and testable.
// See bodhyanga-linkedin/05-mara-five-interfaces.md for the rationale.
type Agent interface {
	// ID returns the unique ID of the agent. This is the bus address used
	// as Message.To for routing. Every agent in Bodhyanga hardcodes its ID as a
	// constant string (e.g. "diagnostic_supervisor", "diagnostician",
	// "human_review"). Tests assert on these IDs; the supervisor's state
	// machine and the governance composer also reference them by name.
	// Changing an ID is a breaking change.
	ID() string

	// Name is a human-readable label used in logs ("[supervisor]
	// starting...") and any UI surface. Unlike ID, Name can change
	// without breaking the platform — it's purely descriptive.
	Name() string

	// Capabilities describes what the agent can do. Returned strings are
	// queryable via registry.FindByCapability — used today by
	// inbox_triage to discover a primary-care agent at runtime rather
	// than hardcoding a peer ID. Stable capability strings are part of
	// the agent's public contract; see docs/agents.md § Capability index
	// for the canonical list.
	Capabilities() []string

	// HandleMessage processes an incoming message and may emit zero or
	// more messages in response. Contract:
	//
	//   - Returning nil, nil means "handled, no follow-up" (common for
	//     terminal agents like diagnostic_supervisor on type=diagnosis,
	//     or human_review which always parks the message).
	//   - Returning a non-nil error tells the orchestrator the agent
	//     failed; the orchestrator records a KindAgentError audit event
	//     (with err.Error() only — never msg.Content, to avoid PHI
	//     leakage in the audit sink) and drops any partial outputs.
	//   - Returning []Message{...} means "publish each of these back
	//     onto the bus". The caller is the orchestrator; agents do NOT
	//     publish directly.
	//
	// The ctx parameter carries deadline / cancellation. LLM-backed
	// agents must honour it (see agents/diagnostician_llm.go). Rule-based
	// agents typically ignore it because their work is microseconds.
	HandleMessage(ctx context.Context, msg Message, env Environment) ([]Message, error)
}

// Environment provides agents with access to shared services like memory,
// tools, and observability.
//
// This is interface 5 of MARA's five — and the most carefully minimal one.
// Keeping it tiny is a deliberate architectural choice from
// docs/architecture.md § Environment and Logger: the orchestrator hands the
// same Environment to every agent's HandleMessage. **Extend by
// composition** — add fields to your concrete type (see
// orchestration.SimpleEnvironment), give it richer methods, then type-
// assert in the agent when you need them. This pattern keeps the interface
// stable while letting deployments inject memory, LLM clients, FHIR
// clients, secret stores, KMS clients, etc.
//
// In a production platform, the concrete Environment passed at startup
// often grows into a service-locator that exposes things like:
//   - Memory (short-term scratchpad, long-term vector store, conversation
//     state)
//   - Tools (search, code execution, retrieval, DB access)
//   - Identity / security context (who asked, what permissions apply)
//   - Observability (structured logging, tracing, metrics)
//   - Evaluation hooks (rubrics, test harness, golden datasets)
//
// This repo keeps Environment minimal on purpose and expands by
// composition rather than baking in assumptions. If you find yourself
// wanting to add a method here, the answer is almost always "define your
// own concrete type that embeds SimpleEnvironment, add fields and methods
// to that, and type-assert in the agent that needs them."
type Environment interface {
	// Now returns the current wall-clock time. Agents must call this
	// instead of time.Now() so tests can inject a FakeClock for
	// deterministic timestamps. RPM alert thresholds, HITL SLA
	// expiry, and audit event timestamps all flow through here.
	Now() time.Time

	// Logf is the structured-or-not logging seam. Production wiring is
	// expected to wrap a phi.LoggerWrapper around the inner logger so
	// PHI patterns (SSN, MRN, US phone, ISO date) are redacted before
	// they reach any third-party sink. See docs/architecture.md
	// § Logger composition.
	Logf(format string, args ...any)
	// Additional services (memory, tools, policies, etc.) can be added
	// by giving your concrete Environment type extra methods/fields and
	// type-asserting at the agent site — NOT by extending this interface.
}
