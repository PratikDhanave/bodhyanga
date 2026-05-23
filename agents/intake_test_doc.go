// intake_test.go — unit tests for the intake agent.
//
// Overview:
//   Verifies that intake decodes a PatientPresentation, constructs CaseState
//   with metadata (case_id, max_cost_usd, gold_diagnosis), routes to
//   rpm_monitor, and returns an error on invalid JSON.
//
// Key declarations:
//   - TestIntakeBuildsCaseStateAndRoutesToRPM: happy path assertions.
//   - TestIntakeReturnsErrorOnInvalidPresentation: malformed input handling.
//
// See doc.go for package-level documentation.

package agents
