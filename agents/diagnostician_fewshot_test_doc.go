// diagnostician_fewshot_test.go — tests for FewShotDiagnostician.
//
// Overview:
//   Uses the llm.Mock client and the fakeKnowledge stub (defined in
//   testhelpers_knowledge_test.go) to verify the four documented behaviours
//   of FewShotDiagnostician: an Examples block is prepended to the user
//   message when shots are retrieved; nil Shots leaves the base prompt
//   untouched; citations whose score is below the 0.3 floor are dropped; and
//   on any LLM error the rule-based fallback still emits a proposal to
//   reasoning_verifier.
//
// Key declarations:
//   - TestFewShot_InjectsExamplesIntoPrompt
//   - TestFewShot_NoStoreSkipsInjection
//   - TestFewShot_LowScoreCitationsDropped
//   - TestFewShot_FallsBackOnLLMError
//
// See doc.go for package-level documentation.

package agents
