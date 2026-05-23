// inbox_triage_llm_test.go — tests for LLMInboxTriage.
//
// Overview:
//   Uses the llm.Mock client to verify the LLM inbox-triage agent's success
//   path (refill classification routed to refill_manager) and its two
//   fallback triggers (LLM error, malformed JSON). All three cases assert
//   that the produced message matches the same refill_request contract the
//   rule-based agent emits, so downstream agents remain unchanged.
//
// Key declarations:
//   - TestLLMInboxTriageSuccessEmitsExpectedMessage
//   - TestLLMInboxTriageFallsBackOnLLMError
//   - TestLLMInboxTriageFallsBackOnBadJSON
//
// See doc.go for package-level documentation.

package agents
