// orchestrator.go — message-driven coordinator that wires agents, bus, and governance.
//
// Overview:
//   Orchestrator is the single point where governance runs before any agent
//   sees a message and where agents' output messages get re-published onto
//   the bus to drive the next hop. It carries no agent-specific logic; it
//   relies entirely on the registry, bus, and policy injected at construction.
//   SimpleEnvironment is the small Environment implementation orchestrators
//   hand to each HandleMessage call.
//
// Key declarations:
//   - Orchestrator: coordinator holding registry, bus, policy, and environment.
//   - NewOrchestrator: dependency-injection constructor.
//   - Orchestrator.Start: subscribes every registered agent to the bus and applies policy on each hop.
//   - Orchestrator.Refresh: hot-reload entry point — subscribes agents added to the registry after Start.
//   - Orchestrator.Watch: opt-in background loop that calls Refresh on a ticker until ctx is cancelled.
//   - Orchestrator.subscribeAgentLocked: internal helper that wires a single agent's bus subscription (caller holds o.mu).
//   - SimpleEnvironment: Logger + Clock-backed agent.Environment implementation.
//   - SimpleEnvironment.Now / SimpleEnvironment.Logf: environment methods.
//
// See doc.go for package-level documentation.

package orchestration
