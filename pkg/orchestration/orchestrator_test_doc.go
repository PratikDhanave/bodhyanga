// orchestrator_test.go — integration-style tests for the orchestration package.
//
// Overview:
//   These tests exercise Orchestrator's message pump end-to-end against the
//   in-memory registry and in-memory bus, asserting the orchestrator's contract:
//   policy runs before agent invocation, agent outputs are republished on the
//   bus, handler errors are logged but do not kill the pump, unknown targets
//   are routed to nothing, and SimpleEnvironment forwards to its Clock/Logger.
//
// Key declarations:
//   - TestOrchestrator_DispatchesMessageToTargetedAgent
//   - TestOrchestrator_AgentOutputsAreRepublished
//   - TestOrchestrator_PolicyDenyShortCircuitsAgent
//   - TestOrchestrator_PolicyAllowReachesAgent
//   - TestOrchestrator_HandlerErrorIsLoggedAndPipelineContinues
//   - TestOrchestrator_UnknownAgentIDIsHandledGracefully
//   - TestOrchestrator_StartIsIdempotent
//   - TestSimpleEnvironment_NowReturnsClockTime
//   - TestSimpleEnvironment_LogfForwardsToLogger
//   - stubAgent, fakeClock, allowAll, denyType helper types.
//
// See doc.go for package-level documentation.

package orchestration
