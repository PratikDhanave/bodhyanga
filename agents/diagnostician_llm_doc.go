// diagnostician_llm.go — LLM-backed diagnostician agent with rule-based fallback.
//
// Overview:
//   LLMDiagnostician implements agent.Agent under the same ID ("diagnostician")
//   as the deterministic DiagnosticianAgent, so the supervisor's routing is
//   unchanged. It serialises the CaseState as JSON, asks the configured
//   llm.Client for a structured DiagnosisProposal, and emits the same
//   "diagnosis_proposal" message that the rule-based agent does. Any LLM
//   failure (timeout, transport error, JSON parse failure, low confidence)
//   delegates to the Fallback agent so the pipeline always finalises.
//
// Key declarations:
//   - LLMDiagnostician: struct with Client, Fallback, Timeout, MaxTokens, MinConfidence
//   - LLMDiagnostician.HandleMessage: agent entry point
//   - diagnosticianSystemPrompt: cached system prompt sent on every call
//   - diagnosisSchema: JSON Schema for DiagnosisProposal
//
// See doc.go for package-level documentation.

package agents
