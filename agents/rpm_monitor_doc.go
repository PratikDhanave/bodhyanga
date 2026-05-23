// rpm_monitor.go — remote patient monitoring ingest and vitals triage.
//
// Overview:
//   Evaluates RPM observations attached to the case state, flags alerts
//   (e.g. elevated BP, weight gain, low SpO2), and forwards the updated
//   state to the diagnostic_supervisor.
//
// Key declarations:
//   - RPMMonitorAgent: stateless RPM triage agent.
//   - RPMMonitorAgent.HandleMessage: evaluates alerts and emits case_state.
//
// See doc.go for package-level documentation.

package agents
