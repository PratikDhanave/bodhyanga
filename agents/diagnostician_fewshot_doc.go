// diagnostician_fewshot.go — in-context few-shot variant of LLMDiagnostician.
//
// Overview:
//   FewShotDiagnostician wraps an LLMDiagnostician (Base) and a second
//   knowledge.Store (Shots) holding clinician-approved past cases. On every
//   HandleMessage it retrieves the top-K relevant past cases, drops anything
//   with score < 0.3, and prepends an "Examples (past clinician-approved
//   cases):" block to the user message produced by Base.buildRequest. The
//   base RAG Evidence block (when Base.Knowledge is configured) remains
//   intact — Examples and Evidence stack. Completion, citation
//   reconciliation, confidence floor, and the rule-based fallback all flow
//   through Base.completeAndDecode and Base.runFallback unchanged. Search
//   errors are logged and swallowed; the LLM is still called without
//   Examples rather than firing the fallback prematurely.
//
// Key declarations:
//   - FewShotDiagnostician: composes Base + Shots store
//   - fewShotMinScore: 0.3 score floor for injected examples
//   - renderShots(citations) string: builds the prompt block (header asserted by tests)
//
// See doc.go for package-level documentation.

package agents
