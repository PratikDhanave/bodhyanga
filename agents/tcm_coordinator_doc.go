// tcm_coordinator.go — transitional care management after discharge.
//
// Overview:
//   Receives discharge_event messages, reconciles home + new medications,
//   creates nurse follow-up tasks on a risk-adjusted schedule, and emits a
//   patient-facing welcome (optionally HITL-gated) through engagement.
//
// Key declarations:
//   - TCMCoordinatorAgent: agent with Tasks queue, Registry, and HITL flag.
//   - TCMCoordinatorAgent.HandleMessage: orchestrates med rec and follow-ups.
//   - priorityForDay: maps days-out plus risk to task priority.
//
// See doc.go for package-level documentation.

package agents
