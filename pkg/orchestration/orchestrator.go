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
		}
	})
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

