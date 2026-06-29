// Package gateway packages the orchestrator's per-message admission control —
// governance evaluation plus audit recording — into a reusable Pipeline so a
// per-agent runtime can enforce the exact same safety boundary the monolith
// enforces today in pkg/orchestration.
//
// Why this exists (agent decoupling, Phase 1)
//
// In the monolith, pkg/orchestration.Orchestrator.Start wires governance,
// audit, agent execution, and fan-out into a single closure that runs once per
// message. When agents become separate binaries (each its own process), there
// is no shared orchestrator to run that closure. Each agent's runtime must
// reproduce Steps 1–2 of it — governance admit/deny and the audit trail —
// before invoking the agent's HandleMessage. gateway.Pipeline.Admit IS that
// reproduction, byte-for-byte faithful to orchestrator.go so a decoupled agent
// behaves identically to the same agent inside the monolith.
//
// Fidelity contract (mirrors pkg/orchestration.Orchestrator.Start):
//
//  1. Policy.Evaluate (nil policy ⇒ allow-all). A non-nil error is treated
//     as a deny (fail closed). A deny records a KindPolicyDecision audit
//     event and blocks the message. An allow records a KindPolicyDecision
//     ONLY for clinical message types (see DefaultClinicalTypes) — matching
//     the orchestrator's deliberate choice not to flood the audit log with
//     bookkeeping allows.
//  2. Extra middlewares (content safety, circuit breaker, …) run on the
//     allow path; a block is fail-closed and audited.
//  3. A KindMessage audit event is recorded immediately before the agent is
//     admitted, so a panicking agent still leaves an "invoked X with Y" trail.
//
// What gateway does NOT do: it never copies msg.Content into an audit event
// (PHI stays on the wire, never in the log — audit.FromMessage/pickStrings
// enforce this), and it does not run the eval-mirror side-channel (that is a
// standalone subscriber, not per-agent admission).
package gateway
