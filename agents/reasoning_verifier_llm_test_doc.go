// reasoning_verifier_llm_test.go — tests for LLMReasoningVerifier.
//
// Overview:
//   Uses the llm.Mock client to verify the LLM verifier's success paths
//   (diagnosis emitted to the supervisor; review_request emitted to
//   human_review under HITL), its reject path (no message emitted), and
//   its two fallback triggers (LLM error, malformed JSON).
//
// Key declarations:
//   - TestLLMReasoningVerifierSuccessEmitsExpectedMessage
//   - TestLLMReasoningVerifierHITLRoutesToHumanReview
//   - TestLLMReasoningVerifierRejectEmitsNothing
//   - TestLLMReasoningVerifierFallsBackOnLLMError
//   - TestLLMReasoningVerifierFallsBackOnBadJSON
//
// See doc.go for package-level documentation.

package agents
