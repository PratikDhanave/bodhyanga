// reasoning_verifier_llm.go — LLM-backed clinical reasoning verifier with rule-based fallback.
//
// Overview:
//   LLMReasoningVerifier implements agent.Agent under the same ID
//   ("reasoning_verifier") as the deterministic ReasoningVerifierAgent, so
//   the supervisor's routing is unchanged. It asks the configured llm.Client
//   to judge whether a DiagnosisProposal is grounded in the accompanying
//   CaseState evidence and is not over-confident, returning a verified flag
//   plus a one-sentence reason. On verified=true it emits the same
//   "diagnosis" or "review_request" message the rule-based agent does (split
//   on HITL); on verified=false it logs the reject and emits nothing. Any
//   LLM failure (timeout, transport error, JSON parse failure) delegates to
//   the Fallback agent.
//
// Key declarations:
//   - LLMReasoningVerifier: struct with Client, Fallback, HITL, Timeout, MaxTokens
//   - LLMReasoningVerifier.HandleMessage: agent entry point
//   - verifierSystemPrompt: cached system prompt sent on every call
//   - verifierSchema: JSON Schema for the verifier output
//
// See doc.go for package-level documentation.

package agents
