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
//   - Orchestrator: coordinator holding registry, bus, policy, environment, and optional audit/eval sinks.
//   - NewOrchestrator: dependency-injection constructor.
//   - Orchestrator.SetAuditRecorder: optional HIPAA §164.312(b) audit sink wired before Start. Fail-open on Record errors.
//   - Orchestrator.SetEvalStore: optional eval.Store mirror for completed diagnostic cases.
//   - Orchestrator.Start: subscribes every registered agent to the bus, applies policy on each hop, records audit events, and mirrors terminal supervisor hops to eval.
//   - SimpleEnvironment: Logger + Clock-backed agent.Environment implementation.
//   - SimpleEnvironment.Now / SimpleEnvironment.Logf: environment methods.
//
// See doc.go for package-level documentation.

package orchestration
