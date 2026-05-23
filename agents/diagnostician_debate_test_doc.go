// diagnostician_debate_test.go — unit tests for DebateDiagnostician.
//
// Overview:
//   Exercises the multi-expert debate diagnostician across four paths:
//   the happy path where every specialist contributes and the chair
//   synthesises the final proposal, the quorum-failure path where most
//   specialists error and the rule-based fallback fires, the chair-
//   failure path where specialists succeed but the chair's LLM call
//   errors, and a sanity check on DefaultSpecialists().
//
//   Uses scriptedClient (a small llm.Client test double in this file)
//   to dispense distinct responses per call. The chair is identified by
//   its system-prompt prefix ("You are the chair"), which lets the script
//   match the chair reply regardless of the goroutine scheduling order
//   of the specialist fan-out.
//
// Key declarations:
//   - scriptedClient / scriptedReply: per-call scripted llm.Client double
//   - TestDebate_AllSpecialistsContributeChairSynthesises
//   - TestDebate_FallsBackWhenMostSpecialistsFail
//   - TestDebate_FallsBackOnChairFailure
//   - TestDebate_DefaultSpecialistsCountIs4
//
// See doc.go for package-level documentation.

package agents
