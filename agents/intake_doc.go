// intake.go — clinical intake agent that seeds case state for sequential diagnosis.
//
// Overview:
//   Decodes a patient presentation, constructs the initial medical.CaseState
//   (presentation, budget, condition, RPM observations) and routes it to
//   rpm_monitor as the first hop of the diagnostic pipeline.
//
// Key declarations:
//   - IntakeAgent: stateless agent entry point for new cases.
//   - IntakeAgent.HandleMessage: builds CaseState and emits case_state.
//
// See doc.go for package-level documentation.

package agents
