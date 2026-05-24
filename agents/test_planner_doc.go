// test_planner.go — MAI-DxO "Request Test" agent with cost-aware ordering.
//
// Overview:
//   Dispatches a condition-appropriate workup (e.g. troponin+ECG for ACS,
//   BNP+CXR+ECG for CHF, BMP+ketones+ABG for DKA, blood culture+lactate for
//   sepsis) selected by state.Condition, records simulated results, and
//   forwards the updated case state to the cost_guardian for budget
//   validation. Unknown / empty conditions fall back to the historical
//   respiratory workup (CBC, CXR, BMP) so legacy cases continue to land
//   on CAP.
//
// Key declarations:
//   - TestPlannerAgent: implements the diagnostic test planning step.
//   - TestPlannerAgent.HandleMessage: orders tests and routes to cost_guardian.
//   - conditionWorkups / defaultWorkup / workupForCondition: per-condition
//     dispatch table.
//   - simulatedTestResult: condition-aware deterministic stub results for
//     demo cases.
//
// See doc.go for package-level documentation.

package agents
