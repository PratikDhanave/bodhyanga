package registry

// File registry.go: the Registry interface (MARA interface 3 of 5) and its
// in-memory implementation. See doc.go for the package-level overview.
//
// Conceptual split that the architecture relies on:
//
//   - Registry: "Which agents exist and what can they do?" (this package)
//   - Bus:      "How do messages move between agents?" (pkg/comm)
//
// Keeping them separate is what lets you swap one without touching the
// other. In Bodh production deployments the registry might be replaced
// with a Consul/etcd/Kubernetes EndpointSlice-backed implementation when
// agents live in separate pods (see docs/architecture.md § Registry
// semantics); the bus might be swapped to Kafka, NATS, or RabbitMQ
// independently — and no agents/*.go file changes either way.

import (
	"context"
	"errors"
	"sync"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// ErrNotFound is returned when an agent cannot be found.
//
// Registry lookups are a frequent operation in orchestrators / routers.
// Having a sentinel error allows callers to distinguish "not found" from
// infrastructure errors via errors.Is. The orchestrator currently does
// not call Get (it uses List to snapshot at Start), but capability-based
// dispatch through FindByCapability can return zero matches — callers
// should treat that as "no agent for this capability today" and degrade,
// not as an error.
var ErrNotFound = errors.New("agent not found")

// Registry stores and exposes agents for discovery.
//
// Conceptually, this is the "agents registry" building block in the
// reference architecture. It is deliberately separate from the comm bus:
//
//   - The registry answers: "Which agents exist and what can they do?"
//   - The bus answers: "How do messages move between agents?"
//
// That separation makes it easier to swap either implementation. In
// particular: capability-based dispatch (FindByCapability) lets a
// supervisor delegate to whatever agent can do the work without naming a
// peer hard-coded. See docs/architecture.md § Registry semantics.
type Registry interface {
	// Register makes an agent discoverable.
	//
	// Most systems register at startup, but dynamic registration is
	// also common: ephemeral agents, autoscaling workers, or
	// user-scoped agents. Bodh's cmd/care wires every agent at boot;
	// future hot-reload via Orchestrator.Watch refreshes the bus
	// subscriptions when a new agent appears here.
	//
	// In-memory semantics (see InMemoryRegistry below): registering
	// the same ID twice REPLACES — no error, no merge. There is no
	// Unregister method by deliberate omission.
	Register(ctx context.Context, a agent.Agent) error

	// Get fetches an agent by ID (its routing address). Returns
	// ErrNotFound when the id is unknown. Used primarily in tests and
	// in HTTP handlers that need to introspect a specific agent's
	// Name() / Capabilities() for the admin UI.
	Get(ctx context.Context, id string) (agent.Agent, error)

	// List returns all registered agents. The orchestrator calls this
	// at Start to snapshot the agent set and subscribe each one. The
	// returned slice is a snapshot — safe to iterate without holding
	// the registry's lock, but the contained agent VALUES are shared
	// (don't mutate their internal state from List callers).
	List(ctx context.Context) []agent.Agent

	// FindByCapability returns all agents that claim a given
	// capability. Coordinators / routers use this for capability-based
	// dispatch — the inbox_triage agent uses it today to find a
	// primary-care agent at runtime rather than hard-coding a peer ID.
	// Returns an empty slice (not nil, not error) when no agent has
	// the capability.
	FindByCapability(ctx context.Context, capability string) []agent.Agent
}

// InMemoryRegistry is a simple thread-safe implementation of Registry.
//
// Concurrency model:
//   - Uses a RWMutex so reads (Get / List / FindByCapability) can proceed
//     concurrently when no Register is in flight. Reads dominate in steady
//     state (every orchestrator subscription, every capability lookup);
//     Register happens at startup and on the rare hot-reload.
//   - Register takes a write lock because it mutates the map.
//
// This is intentionally minimal: it favours clarity over features like
// TTLs, health checks, distributed coordination, or capability versioning.
// Production deployments that need any of those should implement Registry
// against their service-discovery backend rather than wrapping this type.
type InMemoryRegistry struct {
	mu     sync.RWMutex
	agents map[string]agent.Agent
}

// NewInMemory creates a new in-memory registry.
//
// The returned registry is safe for concurrent use. Callers typically
// construct exactly one per process and inject it into the orchestrator
// (see cmd/demo/main.go).
func NewInMemory() *InMemoryRegistry {
	return &InMemoryRegistry{
		agents: make(map[string]agent.Agent),
	}
}

// Register adds or replaces an agent in the registry.
//
// **Replace behaviour:** if the same ID is registered again, the old
// implementation is replaced silently — no error is returned. Why this
// shape:
//
//   - Tests build a fresh orchestrator + fresh registry per case and don't
//     want to worry about cleanup between subtests.
//   - Hot-reload patterns (Orchestrator.Watch) treat the registry as the
//     source of truth — registering an upgraded agent should "win".
//   - The MARA reference architecture leaves duplication policy to the
//     implementation; we picked the simplest one.
//
// Production registries usually have additional controls: agent
// versioning, idempotency tokens, conflict resolution policies. For Bodh
// they're not needed because deployment is single-process today.
//
// Note: there is no Unregister. Removing an agent from a running
// orchestrator is delicate — tearing down a live subscription while
// messages may still be in flight in the bus goroutines risks lost work.
// Documented non-feature; planned for a future iteration if needed.
func (r *InMemoryRegistry) Register(_ context.Context, a agent.Agent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Defensive check: nil agents or agents with empty IDs would
	// poison the map (empty string is the bus broadcast key in
	// pkg/comm — registering an agent there would deliver every
	// message in the system to that agent, a security disaster).
	if a == nil || a.ID() == "" {
		return errors.New("invalid agent")
	}
	r.agents[a.ID()] = a
	return nil
}

// Get returns an agent by ID.
//
// ID is the agent's routing address; it is what appears in
// protocol.Message.To. The orchestrator does NOT use Get for dispatch
// (it relies on bus.Subscribe by ID + the bus routing) — Get is for
// introspection: HTTP handlers, admin UIs, dynamic policy lookups.
func (r *InMemoryRegistry) Get(_ context.Context, id string) (agent.Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	if !ok {
		return nil, ErrNotFound
	}
	return a, nil
}

// List returns all agents.
//
// **Note:** the returned slice is a snapshot; it is safe for the caller
// to iterate without holding locks. The agent values themselves are NOT
// copied (interfaces wrap pointers), so callers must not mutate the
// agent's internal state from a List loop. Today no agent in Bodh
// exposes mutable state through its interface, so this is a theoretical
// concern.
//
// Order is map-iteration order — non-deterministic. Tests must not assert
// on order. The orchestrator doesn't care about order: it subscribes
// each agent independently.
func (r *InMemoryRegistry) List(_ context.Context) []agent.Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]agent.Agent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
	return out
}

// FindByCapability returns agents that advertise the given capability.
//
// Capability strings are an intentionally lightweight mechanism — just
// match by string-equal across the agent's Capabilities() slice. In
// production, you may want a richer schema (capability name + version
// + input / output types + cost / latency characteristics + security
// classification). That richer schema would be a Capability struct
// instead of a string, plus a more elaborate FindBy* method set.
//
// Usage example (inbox_triage at runtime):
//
//	primaries := reg.FindByCapability(ctx, "primary_care_triage")
//	// dispatch to primaries[0] without hard-coding "pcp_agent" as a peer ID
//
// Important contract: this calls List under the read lock and then
// re-checks each agent without holding the lock. That's safe because the
// agent set itself is the only concurrent-mutable state — Capabilities()
// is conventionally a pure function returning constant data.
func (r *InMemoryRegistry) FindByCapability(ctx context.Context, capability string) []agent.Agent {
	all := r.List(ctx)
	out := make([]agent.Agent, 0, len(all))
	for _, a := range all {
		for _, c := range a.Capabilities() {
			if c == capability {
				out = append(out, a)
				// First match per agent — avoid double-adding
				// an agent that lists the same capability twice
				// (which would itself be a bug, but cheap to
				// defend against).
				break
			}
		}
	}
	return out
}

// Compile-time assertion that InMemoryRegistry satisfies Registry. If this
// stops compiling, the interface and the implementation have drifted.
var _ Registry = (*InMemoryRegistry)(nil)
