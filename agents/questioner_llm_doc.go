// questioner_llm.go — LLM-backed history & symptom questioner with rule-based fallback.
//
// Overview:
//   LLMQuestioner implements agent.Agent under the same ID ("questioner") as
//   the deterministic QuestionerAgent, so the supervisor's routing is
//   unchanged. It serialises the CaseState as JSON, asks the configured
//   llm.Client for 3-5 context-relevant history questions plus the 3 most
//   likely starting hypotheses, and emits the same "questions_complete"
//   message that the rule-based agent does. Any LLM failure (timeout,
//   transport error, JSON parse failure, empty questions list) delegates to
//   the Fallback agent so the pipeline always advances.
//
// Key declarations:
//   - LLMQuestioner: struct with Client, Fallback, Timeout, MaxTokens
//   - LLMQuestioner.HandleMessage: agent entry point
//   - questionerSystemPrompt: cached system prompt sent on every call
//   - questionerSchema: JSON Schema for the questioner output
//
// See doc.go for package-level documentation.

package agents
