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
//
// This set is duplicated in pkg/governance/medical_policy.go
// (RequireCaseIDPolicy.Evaluate). The duplication is intentional — both
// files want the literal map for cheap O(1) lookup and we prefer a copy
// over an import cycle from orchestration into governance for a constant.
// If you add a clinical message type, update BOTH places. See
// docs/architecture.md § Audit pipeline for the rationale on why we
// suppress non-clinical allow events from the audit log.
var clinicalMessageTypes = map[string]bool{
	"presentation":     true,
	"case_state":       true,
	"ask_question":     true,
	"order_test":       true,
	"diagnosis":        true,
	"verify_reasoning": true,
}

// Orchestrator coordinates multiple agents, applying governance and routing messages via a bus.
//
// This type represents the "orchestrator" building block from Microsoft's
// Multi-Agent Reference Architecture (MARA). It is the ONLY place in the
// platform where governance runs — see docs/architecture.md
// § "Orchestrator: the only place where governance runs" — and the only
// place that touches policy, bus, agent.HandleMessage, and the audit/eval
// sinks together. Every clinically interesting behaviour in Bodh is a
// consequence of the closure built inside Start.
//
// Key relationships:
//   - Registry: provides the set of agents and their IDs/capabilities.
//   - Bus: transports protocol messages between components.
//   - Policy: enforces governance rules before work is performed.
//   - Environment: provides shared utilities (logging, time, and future services).
//
// Importantly, Orchestrator does NOT contain agent-specific logic; it is a
// generic coordinator. This keeps the platform stable even as agents change
// — MARA design rule: orchestrator coordinates, agents do not call each
// other. Adding a new agent type, a new condition, or a new care-services
// workflow is zero orchestrator-side change.
type Orchestrator struct {
	// reg is the agent registry snapshotted at Start. Today's
	// implementation captures it once via sync.Once; agents registered
	// after Start are NOT auto-subscribed (hot-reload is on the roadmap).
	reg registry.Registry

	// bus is the transport. Subscribe + Publish are the only two methods
	// the orchestrator calls. In production this is Kafka / NATS /
	// RabbitMQ behind the same Bus interface — see
	// docs/architecture.md § Bus semantics for the swap table.
	bus comm.Bus

	// policy is the composed governance stack (typically a
	// *governance.CompositePolicy). Evaluate runs synchronously before
	// every HandleMessage. A nil policy disables governance entirely —
	// supported only for the trivial cmd/demo entry; cmd/care always
	// wires a real composite.
	policy governance.Policy

	// env is handed to every agent's HandleMessage. It carries Logf and
	// Now. Production deployments wrap the inner Logger in
	// phi.LoggerWrapper before passing it in here (see
	// docs/architecture.md § Environment and Logger).
	env agent.Environment

	// recorder is the optional HIPAA §164.312(b) audit sink. When non-nil,
	// the orchestrator records up to four event Kinds per agent hop:
	//   - KindPolicyDecision (allow on clinical types, or any deny)
	//   - KindMessage        (immediately before HandleMessage)
	//   - KindAgentError     (if HandleMessage returns err)
	// plus KindReviewDecision at the HITL gate (recorded by pkg/hitl,
	// not here). Set via SetAuditRecorder BEFORE Start; setters after
	// Start are silently ignored because the per-agent subscription
	// closures captured the recorder field at Subscribe time.
	//
	// Recording is fail-open: a Record error is logged but never blocks
	// message processing. Audit unavailability is operational; blocking
	// clinical flow because the sink is down is the worst outcome.
	// Documented in docs/governance.md § Fail-open semantics.
	recorder audit.Recorder

	// evalStore is the optional eval.Store the diagnostic supervisor's
	// terminal "diagnosis" hop is mirrored to. Like recorder, fail-open
	// and orthogonal to the agent pipeline. Mirroring lives here (not in
	// the supervisor itself) so the supervisor can stay a zero-value
	// struct and so a single Bodh deployment can swap eval backends
	// without touching the clinical agent code.
	evalStore eval.Store

	// startOnce guarantees Start subscribes agents at most once even when
	// callers invoke it more than once (a real footgun before — double-
	// subscription delivered every message twice to every agent, which
	// for an idempotent agent looks like a 2× cost and for a non-
	// idempotent one corrupts state). sync.Once is the cheapest correct
	// implementation; the trade-off is no "re-subscribe after registry
	// change" path, which we don't need today.
	startOnce sync.Once
}

// NewOrchestrator constructs a new orchestrator.
//
// All four dependencies are required at construction time so the
// orchestrator is a valid coordinator the moment NewOrchestrator returns.
// The optional sinks (recorder, evalStore) are wired afterwards via the
// setters before Start.
//
// Dependency injection is used so you can easily swap implementations:
//   - a distributed registry vs in-memory
//   - Kafka/NATS bus vs in-memory
//   - a more advanced governance engine
//   - a richer environment implementation
//
// Construction is cheap (no goroutines, no I/O); it is safe to call
// during package init or test setup.
func NewOrchestrator(reg registry.Registry, bus comm.Bus, policy governance.Policy, env agent.Environment) *Orchestrator {
	return &Orchestrator{
		reg:    reg,
		bus:    bus,
		policy: policy,
		env:    env,
	}
}

// SetAuditRecorder wires an audit.Recorder for HIPAA §164.312(b) compliance.
//
// CRITICAL ORDERING: call this BEFORE Start. Configuration after Start is
// silently ignored because the per-agent subscriptions capture the recorder
// field via the closure at the moment Subscribe is invoked; a later
// assignment to o.recorder is not seen by those already-running closures.
// This is a deliberate trade-off — capturing at subscribe time avoids a
// per-hop dereference through an atomic pointer, at the cost of
// configurability after Start. If you need post-Start swap, that's a
// future refactor and would need a sync/atomic seam.
//
// Pass nil to disable audit (the default). cmd/demo and cmd/caredemo run
// without a recorder; cmd/care wires the in-memory or Postgres recorder
// depending on BODH_DB_DSN — see docs/deployment.md.
//
// All recorder calls are fail-open: errors are logged via env.Logf and never
// block message processing. See docs/governance.md § Fail-open semantics
// for the rationale.
//
// Added by PR #12 (orchestrator audit wiring) — the change that moved
// HIPAA §164.312(b) audit controls from "partial" to "wired".
func (o *Orchestrator) SetAuditRecorder(r audit.Recorder) {
	o.recorder = r
}

// SetEvalStore wires an eval.Store; the orchestrator mirrors the final
// "diagnosis" hop from the diagnostic supervisor into one InteractionRecord
// per completed case.
//
// Same capture-at-subscribe-time semantics as SetAuditRecorder: call BEFORE
// Start, or the mirror is silently no-op. Same fail-open semantics: a
// Save error is logged, never propagated. The supervisor itself does not
// know about eval — keeping the mirror here is what allows the supervisor
// to stay a zero-value struct and lets operators swap eval backends
// without touching agent code.
//
// Added by PR #12 alongside SetAuditRecorder.
func (o *Orchestrator) SetEvalStore(s eval.Store) {
	o.evalStore = s
}

// Start wiring: subscribe each registered agent to the bus.
//
// Start is idempotent — calling it twice is a no-op (enforced by
// startOnce). The first call snapshots the registry and subscribes every
// agent; subsequent calls return immediately. Agents added to the
// registry after Start are NOT auto-subscribed; that's a hot-reload
// feature for a future iteration (registry.InMemoryRegistry already
// supports re-Register, but the orchestrator does not yet re-subscribe).
//
// What the first call does (step-by-step):
//
//  1. Snapshot the current agent list from the registry.
//  2. For each agent:
//     - Subscribe the bus on the agent's ID.
//     - When a message arrives:
//     a) Evaluate governance policy (allow/deny).
//     b) Invoke Agent.HandleMessage(...) with the message.
//     c) Publish each returned message back onto the bus.
//
// Why this design (the five properties documented in
// docs/architecture.md § Orchestrator):
//
//   - Fail closed. A denied message has no path that reaches an agent.
//     There is no fallback that bypasses policy.
//   - Single audit point. Every governance decision and every agent
//     invocation passes through this closure — instrument here once,
//     not in 17 agents.
//   - Pluggability. New policies, new audit backends, new eval stores
//     compose without touching agents.
//   - No clinical logic. The orchestrator never branches on diagnosis,
//     condition, or test results — pure transport + policy.
//   - Audit is fail-open. Audit-sink errors never block clinical flow.
//
// The closure runs once per Bus.Subscribe match (one goroutine per
// publish in InMemoryBus). Keep work in the closure cheap; expensive
// logic belongs in the agent or as a deliberate async hook.
func (o *Orchestrator) Start(ctx context.Context) {
	o.startOnce.Do(func() {
		// Snapshot agents at start time. We iterate this slice once
		// and never re-read the registry inside the closure — see the
		// note on hot reload above.
		for _, a := range o.reg.List(ctx) {
			// Capture both the agent ID (string, used in many log /
			// audit fields) and the agent reference itself (interface,
			// used to invoke HandleMessage).
			agentID := a.ID()
			// IMPORTANT: copy the agent into a local before the
			// closure captures it. Without this, every closure built
			// in the loop would close over the same loop variable `a`
			// and end up calling HandleMessage on the last agent in
			// the list. This is the classic Go for-range capture
			// pitfall; the local `handler` makes the capture safe.
			handler := a // capture for closure

			// Subscribe this agent on its ID. The bus routes Publish
			// by msg.To, so the handler fires whenever something is
			// addressed to this agent. The unsubscribe function
			// returned by Subscribe is deliberately discarded — the
			// orchestrator does not support per-agent tear-down today.
			o.bus.Subscribe(agentID, func(c context.Context, msg agent.Message) {
				// === Step 1: Governance ===
				//
				// Apply governance BEFORE the agent sees the message. This is
				// the single point in the platform where every clinical message
				// is authorised — see docs/architecture.md § "Orchestrator: the
				// only place where governance runs". A denied message MUST NOT
				// reach the agent (fail-closed per Bodh design rule 4 and
				// aligned with HIPAA §164.312(b) audit-control posture). Policy
				// errors are treated as denies: the alternative — silent
				// admission on policy malfunction — is the worst safety outcome.
				if o.policy != nil {
					// Policy.Evaluate is the synchronous, sub-100µs hot path.
					// Composite.Evaluate iterates child policies fail-fast.
					res, err := o.policy.Evaluate(c, msg)
					if err != nil {
						// Policy error: log and drop. We do NOT record an
						// audit event here today, which is a known minor
						// gap — policy errors are operationally rare and
						// surface in env.Logf, but a follow-up PR could
						// add an audit.Event{Kind=KindPolicyDecision,
						// Decision="error"} to close the loop.
						o.env.Logf("policy error for msg %s: %v", msg.ID, err)
						return
					}
					if res.Decision == governance.DecisionDeny {
						// Deny path: agent never sees the message.
						// Record the deny audit event unconditionally
						// (every deny is signal — operators want them
						// all in the audit log).
						o.env.Logf("message %s denied by policy: %s", msg.ID, res.Reason)
						o.recordAudit(audit.FromPolicyDecision(msg, res))
						return
					}
					// Allow path: continue to step 2. Record the allow
					// event ONLY for clinical message types. Non-clinical
					// allows (system broadcasts, observer telemetry,
					// evaluator rubrics) would otherwise flood the audit
					// log with bookkeeping noise — see
					// docs/architecture.md § Audit pipeline.
					if clinicalMessageTypes[msg.Type] {
						o.recordAudit(audit.FromPolicyDecision(msg, res))
					}
				}

				// === Step 2: Record the agent invocation ===
				//
				// Log + audit BEFORE running HandleMessage. Doing it before
				// means a panicking agent still leaves an audit trail of
				// "we tried to invoke X with message Y" — pkg/comm
				// recovers handler panics via safeHandle, but we want the
				// trail regardless. The audit Event carries only routing
				// + correlator metadata (case_id, tenant_id, etc.); the
				// message Content is never echoed.
				o.env.Logf("agent %s handling message %s from %s", agentID, msg.ID, msg.From)
				o.recordAudit(audit.FromMessage(agentID, msg))

				// === Step 3: Delegate to the agent ===
				//
				// The agent can return zero messages (no follow-up work) or
				// multiple messages (fan-out). The orchestrator does NOT
				// interpret their meaning — it just republishes them to the
				// bus for routing. This is what keeps the orchestrator free
				// of clinical logic and lets new agents drop in without
				// touching this file.
				//
				// HandleMessage runs synchronously on the goroutine pkg/comm
				// spawned in safeHandle; the agent must respect ctx for
				// cancellation. Any panic inside HandleMessage is recovered
				// by pkg/comm.safeHandle (commit 1b8ce62) — the orchestrator
				// does not need its own recover here.
				out, err := handler.HandleMessage(c, msg, o.env)
				if err != nil {
					// Agent returned an error. Log + audit with the err
					// text in ErrorText (never msg.Content — that would
					// leak PHI). No outputs are published; the case
					// terminates prematurely from the supervisor's POV.
					o.env.Logf("agent %s error: %v", agentID, err)
					o.recordAgentError(agentID, msg, err)
					return
				}

				// === Step 4: Eval mirror (side-channel) ===
				//
				// If the diagnostic supervisor just finalized a case
				// (received a "diagnosis" message AND emitted no
				// follow-up), the case is terminal. Mirror it into the
				// eval.Store as one InteractionRecord per case.
				//
				// We don't pass eval.Store into the supervisor itself
				// so the supervisor can stay a zero-value struct; the
				// orchestrator-side wiring keeps the composition shallow
				// and lets operators swap eval backends without touching
				// agent code. The check order — len(out)==0 first — is
				// the cheap discriminator that lets us skip the agentID
				// string compare on the hot path for non-terminal hops.
				if len(out) == 0 && agentID == "diagnostic_supervisor" && msg.Type == "diagnosis" {
					o.recordCaseEval(msg)
				}

				// === Step 5: Fan-out ===
				//
				// Re-enter the message stream. This is what creates the
				// multi-agent flow: one agent's output becomes another
				// agent's input. The bus delivers each Publish in a
				// separate goroutine (InMemoryBus.Publish), so the
				// orchestrator's per-hop work ends at this loop.
				//
				// No backpressure today: a fast supervisor producing many
				// outputs hands them all to the bus immediately. Production
				// buses (Kafka, NATS) provide backpressure at their own
				// layer.
				for _, m := range out {
					o.bus.Publish(c, m)
				}
			})
		}
	})
}

// recordAudit is the fail-open seam every Record() call goes through.
//
// Two guarantees enforced here:
//  1. Nil-recorder is a no-op (the default — cmd/demo and cmd/caredemo
//     run without an audit recorder). The nil check keeps the hot path
//     branch-free for deployments that don't enable audit.
//  2. Record errors are logged via env.Logf and never propagate. The
//     message pipeline continues regardless of audit-sink availability
//     because blocking clinical flow on audit downtime is the worst
//     outcome — see docs/governance.md § Fail-open semantics.
//
// Production sinks (FileStore, Postgres) batch their own writes; the
// per-call cost in the hot path is a lock + append. If audit ever
// becomes a hot-spot, the fix is async batching at the Recorder layer,
// not bypassing this seam.
func (o *Orchestrator) recordAudit(e audit.Event) {
	if o.recorder == nil {
		return
	}
	if err := o.recorder.Record(e); err != nil {
		// Log includes the message ID and Kind so an operator can
		// correlate the failure to the (still successfully processed)
		// message. Never include msg.Content or any payload fields.
		o.env.Logf("audit record failed (msg=%s kind=%s): %v", e.MessageID, e.Kind, err)
	}
}

// recordAgentError records a KindAgentError event. The error message is
// included as ErrorText (never msg.Content) so the audit log reconstructs
// what failed without leaking PHI.
//
// Important: err.Error() can in theory carry PHI if an agent author wrote
// fmt.Errorf("failed to parse %s", msg.Content). Convention is for agent
// errors to be terse and content-free — pkg/agent tests assert this for
// shipped agents. New agent authors: do NOT include msg.Content in
// returned errors.
func (o *Orchestrator) recordAgentError(agentID string, msg agent.Message, err error) {
	if o.recorder == nil {
		return
	}
	// Build the base event from the message (routing + correlator metadata),
	// then mutate Kind and add ErrorText. We reuse FromMessage so the
	// correlator fields (case_id, tenant_id, trace_id, patient_id) are
	// pulled from msg.Metadata via the same pickStrings path used for
	// the success-case KindMessage event — keeps the audit shape uniform.
	e := audit.FromMessage(agentID, msg)
	e.Kind = audit.KindAgentError
	e.ErrorText = err.Error()
	o.recordAudit(e)
}

// recordCaseEval mirrors a completed case into eval.Store.
//
// Called only from the Start closure on the terminal-supervisor edge
// (agentID == "diagnostic_supervisor", msg.Type == "diagnosis", no
// follow-up messages). All errors here are best-effort: the eval mirror
// is observability, not safety. Logging via env.Logf is the only signal
// of failure — never block agent flow on it.
//
// The decoded state and scored metrics are stored as one
// eval.InteractionRecord per case; pkg/eval.ScoreCase computes accuracy,
// cost, and step count from the case state plus the message_steps hint.
func (o *Orchestrator) recordCaseEval(msg agent.Message) {
	if o.evalStore == nil {
		return
	}
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		// Decoding failed — the supervisor emitted a "diagnosis" message
		// whose Content isn't a valid CaseState. Log + continue; this
		// is a supervisor bug, not a case bug, so the agent flow has
		// already completed correctly from the patient's POV.
		o.env.Logf("eval store: decode case state for msg %s: %v", msg.ID, err)
		return
	}
	// message_steps is recorded by the supervisor's existing log; fall back
	// to a sentinel of 0 if missing from metadata. The scoring math tolerates
	// it (a zero step count just means "we don't know how many hops" — the
	// other metrics remain valid).
	//
	// The two type-assertion branches (int and float64) reflect that
	// JSON-decoded metadata round-trips numbers as float64 while
	// in-process callers set them as int. Accept both shapes rather
	// than enforcing one — operators creating bench fixtures often use
	// JSON literals.
	steps := 0
	if msg.Metadata != nil {
		if v, ok := msg.Metadata["message_steps"].(int); ok {
			steps = v
		} else if v, ok := msg.Metadata["message_steps"].(float64); ok {
			steps = int(v)
		}
	}
	metrics := eval.ScoreCase(state, steps)
	// "diagnostic" is the workflow label — distinguishes from care-services
	// records (which the orchestrator does not yet mirror). Adding new
	// workflow labels is a future-PR concern.
	rec := metrics.ToInteractionRecord(state.CaseID, "diagnostic")
	if err := o.evalStore.Save(rec); err != nil {
		o.env.Logf("eval store: save case %s: %v", state.CaseID, err)
	}
}

// SimpleEnvironment is a basic implementation of agent.Environment using
// observability primitives.
//
// Environment exists so agents do not depend on concrete logging/time
// packages — the interface is deliberately minimal (Now + Logf) so it
// never breaks agent contracts when richer concrete types are added.
// Extend by composition: deployments needing a memory store, FHIR client,
// LLM client, or secret store add fields to their own concrete type and
// type-assert in the agent when they need them. See docs/architecture.md
// § Environment and Logger.
//
// Production deployments typically wrap Logger in phi.LoggerWrapper before
// passing it in here — that wrapping is where the first-line PHI scrub
// happens (the orchestrator never has to know about PHI directly).
type SimpleEnvironment struct {
	// Logger is the underlying log sink. nil falls back to fmt.Printf
	// (only useful in tests / one-off scripts). Production: wrap with
	// phi.LoggerWrapper around an observability.NewStdLogger() before
	// passing in.
	Logger observability.Logger

	// Clock is the time source. nil falls back to time.Now().UTC().
	// Tests inject a FakeClock for deterministic timestamps; agents call
	// env.Now() rather than time.Now() so they pick up the injected
	// clock automatically.
	Clock observability.Clock
}

// Now returns the current time.
//
// Orchestrators and agents use time for timestamps, timeouts, and ordering
// hints. The Clock abstraction makes this deterministic in tests — pass a
// FakeClock with a fixed T, and every env.Now() in the case will return
// that fixed instant. Without a Clock, the method falls back to
// time.Now().UTC() so callers never need to nil-check before invoking.
//
// UTC is the convention everywhere in Bodh — no audit/event/log query
// should ever need to do timezone math.
func (e *SimpleEnvironment) Now() time.Time {
	if e.Clock == nil {
		return time.Now().UTC()
	}
	return e.Clock.Now()
}

// Logf logs a formatted message.
//
// Logging is intentionally "printf-style" for simplicity. In production
// you'd typically use structured logging with fields like trace_id,
// agent_id, msg_id, and message_type — the observability.Logger interface
// supports that; SimpleEnvironment is the demo shape.
//
// Fallback behaviour: a nil Logger writes to stdout via fmt.Printf. The
// fallback exists so tests and quick scripts don't have to construct a
// logger first; production deployments should always wire a real Logger
// (typically wrapped in phi.LoggerWrapper for PHI redaction).
func (e *SimpleEnvironment) Logf(format string, args ...any) {
	if e.Logger == nil {
		fmt.Printf(format+"\n", args...)
		return
	}
	e.Logger.Printf(format, args...)
}

// Compile-time assertion that *SimpleEnvironment satisfies agent.Environment.
// If the Environment interface ever grows a new required method, this line
// breaks the build — which is what we want, so the orchestrator and every
// agent that depends on this concrete type get caught together.
var _ agent.Environment = (*SimpleEnvironment)(nil)
