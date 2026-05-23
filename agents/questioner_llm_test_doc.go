// questioner_llm_test.go — tests for LLMQuestioner.
//
// Overview:
//   Uses the llm.Mock client to verify the LLM questioner's success path
//   and its two fallback triggers (LLM error, malformed JSON). The success
//   path also asserts the system prompt is cached and that the structured
//   JSON schema is included in the request.
//
// Key declarations:
//   - TestLLMQuestionerSuccessEmitsExpectedMessage
//   - TestLLMQuestionerFallsBackOnLLMError
//   - TestLLMQuestionerFallsBackOnBadJSON
//
// See doc.go for package-level documentation.

package agents
