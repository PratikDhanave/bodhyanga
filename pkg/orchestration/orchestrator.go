package orchestration

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

// clinicalMessageTypes mirrors the gated types in
// governance.RequireCaseIDPolicy. The orchestrator records policy *allows*
// only for these types to keep the audit log signal-rich for HIPAA review
// (every non-clinical allow would otherwise drown out real decisions).
var clinicalMessageTypes = map[string]bool{
	"presentation":    true,
	"case_state":      true,
	"ask_question":    true,
	"order_test":      true,
	"diagnosis":       true,
	"verify_reasoning": true,
}

// Orchestrator coordinates multiple agents, applying governance and routing messages via a bus.
//
// This type represents the "orchestrator" building block from the reference
// architecture.
//
// Key relationships:
// - Registry: provides the set of agents and their IDs/capabilities.
// - Bus: transports protocol messages between components.
// - Policy: enforces governance rules before work is performed.
// - Environment: provides shared utilities (logging, time, and future services).
//
// Importantly, Orchestrator does NOT contain agent-specific logic; it is a
// generic coordinator. This keeps the platform stable even as agents change.
type Orchestrator struct {
	reg    registry.Registry
	bus    comm.Bus
	policy governance.Policy
	env    agent.Environment

	// recorder is the optional HIPAA §164.312(b) audit sink. When non-nil,
	// the orchestrator records four events per agent hop: policy decision
	// (allow for clinical types / deny always), agent invocation, agent
	// error. Set via SetAuditRecorder before Start; setters after Start are
	// ignored by the already-captured subscription closures.
	//
	// Recording is fail-open: a Record error is logged but never blocks
	// message processing. Audit unavailability is operational; blocking
	// clinical flow because the sink is down is the worst outcome.
	recorder audit.Recorder

	// evalStore is the optional eval.Store the diagnostic supervisor's
	// terminal "diagnosis" hop is mirrored to. Like recorder, fail-open
	// and orthogonal to the agent pipeline.
	evalStore eval.Store

	// startOnce guarantees Start subscribes agents at most once even when
	// callers invoke it more than once (a real footgun before — double-
	// subscription delivered every message twice to every agent).
	startOnce sync.Once
}

// NewOrchestrator constructs a new orchestrator.
//
// Dependency injection is used so you can easily swap implementations:
// - a distributed registry vs in-memory
// - Kafka/NATS bus vs in-memory
// - a more advanced governance engine
// - a richer environment implementation
func NewOrchestrator(reg registry.Registry, bus comm.Bus, policy governance.Policy, env agent.Environment) *Orchestrator {
	return &Orchestrator{
		reg:    reg,
		bus:    bus,
		policy: policy,
		env:    env,
	}
}

// SetAuditRecorder wires an audit.Recorder for HIPAA §164.312(b) compliance.
// Call this before Start; configuration after Start is silently ignored
// because the per-agent subscriptions capture the recorder at subscribe time.
//
// Pass nil to disable audit (the default). cmd/demo and cmd/caredemo run
// without a recorder; cmd/care wires the in-memory or Postgres recorder
// depending on BODH_DB_DSN.
//
// All recorder calls are fail-open: errors are logged via env.Logf and never
// block message processing. See package docs for the rationale.
func (o *Orchestrator) SetAuditRecorder(r audit.Recorder) {
	o.recorder = r
}

// SetEvalStore wires an eval.Store; the orchestrator mirrors the final
// "diagnosis" hop from the diagnostic supervisor into one InteractionRecord
// per completed case. Same backward-compat / fail-open rules as
// SetAuditRecorder.
func (o *Orchestrator) SetEvalStore(s eval.Store) {
	o.evalStore = s
}

// Start wiring: subscribe each registered agent to the bus.
//
// Start is idempotent — calling it twice is a no-op. The first call
// snapshots the registry and subscribes every agent; subsequent calls
// return immediately. Agents added to the registry after Start are NOT
// auto-subscribed; that's a hot-reload feature for a future iteration.
//
// What the first call does (step-by-step):
//
//  1) Snapshot the current agent list from the registry.
//  2) For each agent:
//      - Subscribe the bus on the agent's ID.
//      - When a message arrives:
//          a) Evaluate governance policy (allow/deny).
//          b) Invoke Agent.HandleMessage(...) with the message.
//          c) Publish each returned message back onto the bus.
//
// Why this design:
//
// - The orchestrator is the single place where governance is enforced.
// - Agents do not call each other directly; they communicate by emitting messages.
// - Observability becomes straightforward: all interactions are messages.
func (o *Orchestrator) Start(ctx context.Context) {
	o.startOnce.Do(func() {
		for _, a := range o.reg.List(ctx) {
			agentID := a.ID()
			handler := a // capture for closure
			o.bus.Subscribe(agentID, func(c context.Context, msg agent.Message) {
				// Apply governance *before* the agent sees the message.
				//
				// This is a key pattern: centralize safety checks at the boundary.
				// If a policy denies a message, the agent never processes it.
				if o.policy != nil {
					res, err := o.policy.Evaluate(c, msg)
					if err != nil {
						o.env.Logf("policy error for msg %s: %v", msg.ID, err)
						return
					}
					if res.Decision == governance.DecisionDeny {
						o.env.Logf("message %s denied by policy: %s", msg.ID, res.Reason)
						o.recordAudit(audit.FromPolicyDecision(msg, res))
						return
					}
					// Record allows only for clinical message types; otherwise the
					// log fills with non-PHI bookkeeping decisions.
					if clinicalMessageTypes[msg.Type] {
						o.recordAudit(audit.FromPolicyDecision(msg, res))
					}
				}

				o.env.Logf("agent %s handling message %s from %s", agentID, msg.ID, msg.From)
				o.recordAudit(audit.FromMessage(agentID, msg))

				// Delegate to the agent implementation.
				//
				// The agent can return zero messages (no follow-up work) or multiple
				// messages (fan-out). The orchestrator does not interpret their meaning;
				// it just republishes them to the bus for routing.
				out, err := handler.HandleMessage(c, msg, o.env)
				if err != nil {
					o.env.Logf("agent %s error: %v", agentID, err)
					o.recordAgentError(agentID, msg, err)
					return
				}
				// Side-channel: if the diagnostic supervisor just finalized a
				// case (emitted no follow-up after receiving a "diagnosis"
				// message), record an eval.InteractionRecord. We don't pass
				// the eval.Store into the supervisor itself so it can stay a
				// zero-value struct; orchestrator-side wiring keeps the
				// composition shallow.
				if len(out) == 0 && agentID == "diagnostic_supervisor" && msg.Type == "diagnosis" {
					o.recordCaseEval(msg)
				}
				for _, m := range out {
					// Re-enter the message stream. This is what creates the multi-agent
					// flow: one agent's output becomes another agent's input.
					o.bus.Publish(c, m)
				}
			})
		}
	})
}

// recordAudit is the fail-open seam every Record() call goes through.
// Errors are logged but never propagate to the caller; the message pipeline
// continues regardless of audit-sink availability.
func (o *Orchestrator) recordAudit(e audit.Event) {
	if o.recorder == nil {
		return
	}
	if err := o.recorder.Record(e); err != nil {
		o.env.Logf("audit record failed (msg=%s kind=%s): %v", e.MessageID, e.Kind, err)
	}
}

// recordAgentError records a KindAgentError event. The error message is
// included as ErrorText (never msg.Content) so the audit log reconstructs
// what failed without leaking PHI.
func (o *Orchestrator) recordAgentError(agentID string, msg agent.Message, err error) {
	if o.recorder == nil {
		return
	}
	e := audit.FromMessage(agentID, msg)
	e.Kind = audit.KindAgentError
	e.ErrorText = err.Error()
	o.recordAudit(e)
}

// recordCaseEval mirrors a completed case into eval.Store. Decoding errors
// are swallowed (logged) because the eval mirror is best-effort — never
// block agent flow on it.
func (o *Orchestrator) recordCaseEval(msg agent.Message) {
	if o.evalStore == nil {
		return
	}
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		o.env.Logf("eval store: decode case state for msg %s: %v", msg.ID, err)
		return
	}
	// message_steps is recorded by the supervisor's existing log; fall back
	// to a sentinel of 0 if missing from metadata. The scoring math tolerates
	// it.
	steps := 0
	if msg.Metadata != nil {
		if v, ok := msg.Metadata["message_steps"].(int); ok {
			steps = v
		} else if v, ok := msg.Metadata["message_steps"].(float64); ok {
			steps = int(v)
		}
	}
	metrics := eval.ScoreCase(state, steps)
	rec := metrics.ToInteractionRecord(state.CaseID, "diagnostic")
	if err := o.evalStore.Save(rec); err != nil {
		o.env.Logf("eval store: save case %s: %v", state.CaseID, err)
	}
}

// SimpleEnvironment is a basic implementation of agent.Environment using observability primitives.
//
// Environment exists so agents do not depend on concrete logging/time packages.
// For example, you can replace Logger with a structured logger, or wire a trace
// context into Logf.
type SimpleEnvironment struct {
	Logger observability.Logger
	Clock  observability.Clock
}

// Now returns the current time.
//
// Orchestrators and agents use time for timestamps, timeouts, and ordering hints.
// The Clock abstraction makes this deterministic in tests.
func (e *SimpleEnvironment) Now() time.Time {
	if e.Clock == nil {
		return time.Now().UTC()
	}
	return e.Clock.Now()
}

// Logf logs a formatted message.
//
// Logging is intentionally "printf-style" for simplicity. In production, you'd
// typically use structured logging with fields like trace_id, agent_id, msg_id,
// and message_type.
func (e *SimpleEnvironment) Logf(format string, args ...any) {
	if e.Logger == nil {
		fmt.Printf(format+"\n", args...)
		return
	}
	e.Logger.Printf(format, args...)
}

var _ agent.Environment = (*SimpleEnvironment)(nil)

