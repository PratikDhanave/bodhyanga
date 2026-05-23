package orchestration

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

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

	// startOnce guarantees Start subscribes agents at most once even when
	// callers invoke it more than once (a real footgun before — double-
	// subscription delivered every message twice to every agent).
	startOnce sync.Once

	// mu guards the started flag and the subscribed set. Start() takes it
	// briefly to mark started; Refresh() takes it for the full diff +
	// subscribe pass so concurrent Refresh callers cannot double-subscribe
	// the same agent ID.
	mu         sync.Mutex
	started    bool
	subscribed map[string]struct{}
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
		reg:        reg,
		bus:        bus,
		policy:     policy,
		env:        env,
		subscribed: make(map[string]struct{}),
	}
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
		o.mu.Lock()
		agents := o.reg.List(ctx)
		for _, a := range agents {
			o.subscribeAgentLocked(a)
		}
		o.started = true
		o.mu.Unlock()
	})
}

// Refresh re-lists the registry and subscribes any agents that are not yet
// known to this orchestrator. It returns the IDs that were newly subscribed
// (in registry-iteration order) so callers can log them.
//
// Semantics:
//
//   - Idempotent. Calling Refresh repeatedly with no new agents is a no-op
//     and returns an empty slice.
//   - Calling Refresh before Start is also a no-op; the initial subscribe
//     pass is owned by Start. This keeps the contract simple: Refresh only
//     ever *extends* an already-running orchestrator.
//   - Agents that were REMOVED from the registry between Start and Refresh
//     are intentionally not unsubscribed. Their bus subscriptions remain,
//     which is consistent with the in-memory registry's "register replaces"
//     model — removal is not a first-class operation here, and tearing down
//     a live subscription while messages may be in flight is a separate,
//     more delicate concern.
func (o *Orchestrator) Refresh(ctx context.Context) (newlySubscribed []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.started {
		return nil
	}
	for _, a := range o.reg.List(ctx) {
		id := a.ID()
		if _, ok := o.subscribed[id]; ok {
			continue
		}
		o.subscribeAgentLocked(a)
		newlySubscribed = append(newlySubscribed, id)
	}
	return newlySubscribed
}

// Watch runs Refresh on an interval until ctx is cancelled. It is opt-in;
// most callers will trigger Refresh manually after a known registration
// event. Newly-subscribed agent IDs are logged through the orchestrator's
// environment so operators can see hot-reloads happen in real time.
//
// Watch blocks until ctx is cancelled and then returns. Callers typically
// run it in a goroutine: `go orch.Watch(ctx, time.Second)`.
func (o *Orchestrator) Watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if added := o.Refresh(ctx); len(added) > 0 {
				o.env.Logf("orchestrator: hot-reload subscribed %d new agent(s): %v", len(added), added)
			}
		}
	}
}

// subscribeAgentLocked wires a single agent to the bus and records the
// subscription. The caller MUST hold o.mu.
//
// Splitting this out of Start lets Refresh reuse the exact same subscribe
// closure — including governance evaluation and republish — without
// duplicating the logic.
func (o *Orchestrator) subscribeAgentLocked(a agent.Agent) {
	agentID := a.ID()
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
				return
			}
		}

		o.env.Logf("agent %s handling message %s from %s", agentID, msg.ID, msg.From)

		// Delegate to the agent implementation.
		//
		// The agent can return zero messages (no follow-up work) or multiple
		// messages (fan-out). The orchestrator does not interpret their meaning;
		// it just republishes them to the bus for routing.
		out, err := a.HandleMessage(c, msg, o.env)
		if err != nil {
			o.env.Logf("agent %s error: %v", agentID, err)
			return
		}
		for _, m := range out {
			// Re-enter the message stream. This is what creates the multi-agent
			// flow: one agent's output becomes another agent's input.
			o.bus.Publish(c, m)
		}
	})
	o.subscribed[agentID] = struct{}{}
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

