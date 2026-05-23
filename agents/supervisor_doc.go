// supervisor.go — MAI-DxO style orchestrator for sequential diagnosis.
//
// Overview:
//   The DiagnosticSupervisor routes a case through ask -> test -> verify ->
//   diagnose, handles cost_guardian denials by dropping the most expensive
//   test, and finalizes the case with eval metrics when a verified diagnosis
//   arrives. Also hosts shared metadata helpers used by other agents.
//
// Key declarations:
//   - DiagnosticSupervisor: orchestrator agent for the virtual physician panel.
//   - DiagnosticSupervisor.HandleMessage: dispatches by message type.
//   - PanelReport: formats a human-readable case summary.
//   - copyMeta / metadataString / metadataFloat / metadataInt: shared meta helpers.
//
// See doc.go for package-level documentation.

package agents
