// diagnostician_reflexion.go — Reflexion / Chain-of-Verification (CoVe)
// diagnostician.
//
// Overview:
//   ReflexionDiagnostician composes an LLMDiagnostician (the Base field) for
//   the initial draft, then runs one or more critique→revise rounds against
//   the same llm.Client. Each round asks the LLM to act as a clinical
//   reasoning verifier: it produces verification questions, answers them from
//   the CaseState evidence (and the same retrieved RAG citations the draft
//   used), then either accepts the proposal (verified=true) or supplies a
//   revised DiagnosisProposal. The loop ends on the first verified=true, on a
//   verified=false response with no revision attached, on a critique error
//   (best-effort: keep current proposal), or when MaxRounds is hit. The final
//   proposal is emitted to reasoning_verifier with the same envelope the base
//   diagnostician uses, so supervisor routing is unchanged.
//
// Key declarations:
//   - ReflexionDiagnostician: struct with Base, MaxRounds, Timeout, MaxTokens
//   - ReflexionDiagnostician.HandleMessage: agent entry point
//   - critiqueOutput / critiqueQA: JSON shape returned by the critique call
//   - reflexionCritiqueSystemPrompt: cached system prompt for the verifier role
//   - reflexionCritiqueSchema: JSON Schema with nullable revised_proposal
//
// See doc.go for package-level documentation.

package agents
