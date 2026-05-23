// cdm_planner_test.go — unit tests for the CDM planner agent.
//
// Overview:
//   Exercises direct and HITL modes of CDMPlannerAgent, asserting that
//   direct mode emits care_plan_ready to engagement plus a panel_manager
//   track message, and that HITL mode wraps engagement in a review_request
//   with the expected envelope metadata.
//
// Key declarations:
//   - cdmRequestMsg: helper to build a cdm_request agent.Message.
//   - TestCDMPlannerDirectMode: asserts direct-mode routing and registry update.
//   - TestCDMPlannerHITLMode: asserts review envelope keys are populated.
//
// See doc.go for package-level documentation.

package agents
