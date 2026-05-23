// diagnostician_debate.go — multi-expert debate diagnostician.
//
// Overview:
//   DebateDiagnostician wraps an LLMDiagnostician and runs N parallel
//   specialist personas (Cardiology, Infectious Disease, Pulmonology,
//   Emergency Medicine by default) against the same CaseState. Each
//   specialist receives the base diagnostician system prompt prefixed with
//   a persona statement so the LLM frames its proposal from that angle.
//   A "chair" LLM call then picks or synthesises the single most likely
//   diagnosis from the surviving specialist proposals. The agent's ID
//   remains "diagnostician" so supervisor routing is unchanged.
//
//   Behaviour:
//     - Builds the base llm.Request once via Base.buildRequest so RAG
//       retrieval, JSON schema, and citation reconciliation are shared.
//     - Fans specialist calls out under a semaphore-capped goroutine pool
//       (max 4 in-flight) with a per-call timeout.
//     - If fewer than half of the specialists return a usable proposal,
//       delegates to Base.runFallback (rule-based agent).
//     - On chair success, emits diagnosis_proposal to reasoning_verifier
//       exactly as LLMDiagnostician does.
//     - On chair failure, low confidence, or JSON parse error, delegates
//       to Base.runFallback.
//
// Key declarations:
//   - DebateDiagnostician: struct with Base, Specialists, ChairPrompt,
//     Timeout, MaxTokens
//   - Specialist: name + 1-2 sentence persona description
//   - DefaultSpecialists: returns the 4-specialist default panel
//   - DebateDiagnostician.HandleMessage: agent entry point
//   - cloneRequestForSpecialist / buildChairRequest: internal helpers
//   - debateChairSystemPrompt: default cached chair system prompt
//   - specialistResult: package-scope struct shared between fan-out and
//     the chair-request builder (Go does not let methods accept
//     function-local types)
//
// See doc.go for package-level documentation.

package agents
