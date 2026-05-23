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
//   - LLMDiagnostician: struct with Client, Fallback, Timeout, MaxTokens,
//     MinConfidence, Knowledge (optional knowledge.Store for RAG), RetrievalK
//   - LLMDiagnostician.HandleMessage: agent entry point
//   - LLMDiagnostician.buildRequest / completeAndDecode: split helpers reused
//     by the vision variant
//   - buildRetrievalQuery: composes presentation + condition + test results +
//     hypotheses into a single retrieval query string
//   - diagnosticianSystemPrompt / diagnosticianSystemPromptRAG: cached system
//     prompts (non-RAG vs RAG-enabled)
//   - diagnosisSchema / diagnosisSchemaRAG: JSON Schemas (with and without
//     optional citations)
//
// See doc.go for package-level documentation.

package agents
