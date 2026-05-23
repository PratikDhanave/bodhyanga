// diagnostician_llm_test.go — tests for LLMDiagnostician.
//
// Overview:
//   Uses the llm.Mock client to verify the LLM diagnostician's success path
//   and each of its three documented fallback triggers (LLM error, malformed
//   JSON, low confidence). The success path also asserts that the system
//   prompt is sent with CacheSystem=true and that the structured JSON schema
//   is included in the request.
//
// Key declarations:
//   - TestLLMDiagnosticianSuccessEmitsProposalToVerifier
//   - TestLLMDiagnosticianFallsBackOnLLMError
//   - TestLLMDiagnosticianFallsBackOnBadJSON
//   - TestLLMDiagnosticianFallsBackOnLowConfidence
//
// See doc.go for package-level documentation.

package agents
