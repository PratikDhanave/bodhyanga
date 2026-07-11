package runtime

// File runtime.go: the Runtime type and its per-message loop. See doc.go for
// how these five steps map back to orchestrator.Start.

import (
	"context"
	"errors"
	"sync"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/gateway"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// Runtime hosts exactly one agent in its own process and runs the per-message
// loop that the monolith's orchestrator would otherwise run on that agent's
// behalf. Construct it with plain field assignment; Agent, Bus, Pipeline, and
// Env are required (Run reports which are missing).
type Runtime struct {
	// Agent is the single agent this process hosts. Run subscribes the bus on
	// Agent.ID(); HandleMessage is invoked for every admitted message.
	Agent agent.Agent

	// Bus is the transport — a NATS bus in a decoupled deployment, an
	// InMemoryBus in tests. Runtime subscribes on Agent.ID() and publishes the
	// agent's outputs; it never subscribes to peer agents.
	Bus comm.Bus

	// Pipeline is the admission boundary (governance + audit). Its AgentID
	// should equal Agent.ID() so audit events attribute to the right agent.
	// A zero-value Pipeline admits everything — the in-process/test default.
	Pipeline *gateway.Pipeline

	// Env is handed to Agent.HandleMessage. It carries Now and Logf and,
	// by composition, any services the concrete agent type-asserts for.
	Env agent.Environment

	// Log is an optional diagnostics logger for the loop itself (subscribe /
	// shutdown lines). Nil silences it. It must never be handed msg.Content.
	Log func(format string, args ...any)

	// startOnce makes Run single-shot; stopFn is the idempotent teardown the
	// first successful Run installs. Both zero-init, so Runtime stays
	// constructible by plain field assignment.
	startOnce sync.Once
	stopFn    func()
}

// Run subscribes the agent on the bus and returns a stop function that
// unsubscribes it. It does not block: the bus drives the handler on its own
// goroutines (InMemoryBus) or consumer (NATSBus).
//
// Lifecycle:
//   - Run is single-shot. A second call returns an error rather than
//     double-subscribing — the bus does not dedup subscriptions (see
//     pkg/comm/bus.go), so a second Subscribe would double-process every
//     message (two HandleMessage calls, two audit events, doubled fan-out).
//     This mirrors orchestrator.startOnce.
//   - Teardown fires on whichever comes first: the returned stop() being
//     called, or ctx being cancelled. Both are idempotent and safe to combine
//     (the common shape is `defer stop()` alongside a SIGTERM-cancelled ctx).
//     Binding to ctx means a decoupled agent process drains cleanly on
//     shutdown even if its main forgets stop().
//
// Run validates required wiring up front — a nil Agent, Bus, Pipeline, or Env
// is a programming error, returned rather than deferred to the first message.
// A validation failure does NOT consume the single-shot guard, so a corrected
// retry can still start the runtime.
func (r *Runtime) Run(ctx context.Context) (stop func(), err error) {
	if r.Agent == nil {
		return nil, errors.New("runtime: Agent is required")
	}
	if r.Bus == nil {
		return nil, errors.New("runtime: Bus is required")
	}
	if r.Pipeline == nil {
		return nil, errors.New("runtime: Pipeline is required")
	}
	if r.Env == nil {
		return nil, errors.New("runtime: Env is required")
	}

	subscribed := false
	r.startOnce.Do(func() {
		subscribed = true
		id := r.Agent.ID()
		r.logf("runtime[%s]: subscribing (%s)", id, r.Agent.Name())
		unsub := r.Bus.Subscribe(id, r.handle)

		// done lets the ctx watcher exit when stop() is called, so it never
		// outlives the subscription (no goroutine leak for a never-cancelled
		// ctx). stopOnce makes teardown idempotent across stop()+ctx.Done().
		done := make(chan struct{})
		var stopOnce sync.Once
		teardown := func() {
			stopOnce.Do(func() {
				r.logf("runtime[%s]: unsubscribing", id)
				close(done)
				unsub()
			})
		}
		go func() {
			select {
			case <-ctx.Done():
				teardown()
			case <-done:
			}
		}()
		r.stopFn = teardown
	})
	if !subscribed {
		return nil, errors.New("runtime: Run already called")
	}
	return r.stopFn, nil
}

// handle is the per-message loop — the single-agent equivalent of the closure
// in orchestrator.Start. It is a comm.Handler: the bus invokes it (recovering
// panics via safeHandle) once per message addressed to this agent.
func (r *Runtime) handle(ctx context.Context, msg protocol.Message) {
	// === Steps 1–2: Admission (governance + audit) ===
	// Admit reproduces the orchestrator's govern-then-audit-invoke prologue.
	// A false return means the message was blocked (and, where applicable,
	// audited by the pipeline); the agent must never see it.
	if !r.Pipeline.Admit(ctx, msg) {
		return
	}

	// === Step 3: Delegate to the agent ===
	out, err := r.Agent.HandleMessage(ctx, msg, r.Env)
	if err != nil {
		r.onAgentError(ctx, msg, err)
		return
	}

	// === Step 5: Fan-out ===
	// Republish the agent's outputs. Each lands on the bus addressed to its
	// own To; the recipient agent's Runtime admits it on receipt — governance
	// runs on receive, not on publish, exactly as in the monolith.
	for _, m := range out {
		r.Bus.Publish(ctx, m)
	}
}

// onAgentError handles a non-nil error from Agent.HandleMessage.
//
// It does exactly two things, and deliberately no more:
//
//  1. Audit the failure. The monolith records a KindAgentError event for every
//     failed handler (orchestrator.recordAgentError); a decoupled agent must
//     not lose that trail. RecordAgentError carries err.Error() only — never
//     msg.Content (PHI stays off the audit log).
//  2. Drop the outputs. The case terminates from the supervisor's point of
//     view, exactly as in the monolith (orchestrator.go Step 3 returns on
//     error without publishing).
//
// This is the fidelity choice: pkg/runtime's contract (see doc.go) is that a
// decoupled agent behaves identically to the same agent inside the monolith,
// and the monolith drops on error. It leaves the "silent death" gap intact —
// the sender waits for a follow-up that never comes until something upstream
// times out.
//
// A tempting alternative is to PUBLISH a failure notification (e.g. Type
// "agent_error" addressed To msg.From) so the supervisor can fail the case fast
// instead of hanging. That is deliberately NOT done here: it is a protocol
// change, not a runtime change. No agent handles an "agent_error" type today —
// the DiagnosticSupervisor would route it to its unknown-type drop path
// (agents/supervisor.go) — so emitting one now would create a message nobody
// consumes. Closing the silent-death gap requires a coordinated change across
// the runtime AND the supervisor (a handler + a case-failure transition) and
// belongs in its own PR. When that lands, any notification message must still
// honour the PHI rule: it must not echo msg.Content.
func (r *Runtime) onAgentError(_ context.Context, msg protocol.Message, err error) {
	r.logf("runtime[%s]: agent error on msg %s: %v", r.Agent.ID(), msg.ID, err)
	r.Pipeline.RecordAgentError(msg, err)
}

func (r *Runtime) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log(format, args...)
	}
}
