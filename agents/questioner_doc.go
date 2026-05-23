// questioner.go — MAI-DxO "Ask Question" agent for history taking.
//
// Overview:
//   Drives rule-based history questions before testing, records simulated
//   answers, seeds the initial differential (condition-aware), and returns
//   the updated case state to the diagnostic_supervisor.
//
// Key declarations:
//   - QuestionerAgent: stateless history & symptom collector.
//   - QuestionerAgent.HandleMessage: asks questions and emits questions_complete.
//   - simulatedAnswer: deterministic stub answers for demo cases.
//
// See doc.go for package-level documentation.

package agents
