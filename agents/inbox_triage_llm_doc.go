// inbox_triage_llm.go — LLM-backed clinical inbox triage with rule-based fallback.
//
// Overview:
//   LLMInboxTriage implements agent.Agent under the same ID ("inbox_triage")
//   as the deterministic InboxTriageAgent, so the supervisor's routing is
//   unchanged. It asks the configured llm.Client to classify each inbox
//   message into refill | prior_auth | urgent_clinical | general and to
//   extract medication / procedure_code entities, then emits the same
//   downstream messages the rule-based agent does (refill_request,
//   prior_auth_request, NurseTask + inbox_routed). Any LLM failure (timeout,
//   transport error, JSON parse failure, invalid category) delegates to the
//   Fallback agent so the pipeline always routes the message.
//
// Key declarations:
//   - LLMInboxTriage: struct with Client, Fallback, Tasks, Timeout, MaxTokens
//   - LLMInboxTriage.HandleMessage: agent entry point
//   - inboxTriageSystemPrompt: cached system prompt sent on every call
//   - inboxTriageSchema: JSON Schema for the classifier output
//
// See doc.go for package-level documentation.

package agents
