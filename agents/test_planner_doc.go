// test_planner.go — MAI-DxO "Request Test" agent with cost-aware ordering.
//
// Overview:
//   Orders a cost-effective workup (CBC, chest X-ray, BMP) for respiratory
//   presentations, records simulated results, and forwards the updated case
//   state to the cost_guardian for budget validation.
//
// Key declarations:
//   - TestPlannerAgent: implements the diagnostic test planning step.
//   - TestPlannerAgent.HandleMessage: orders tests and routes to cost_guardian.
//   - simulatedTestResult: deterministic stub results for demo cases.
//
// See doc.go for package-level documentation.

package agents
