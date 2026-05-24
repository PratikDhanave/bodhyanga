// diagnostician_active_test.go — unit tests for
// ActiveLearningDiagnostician.
//
// Overview:
//   Exercises the active-learning diagnostician across six paths: the
//   agreement happy-path (LLM-only emission), the disagreement-with-HITL
//   path (LLM proposal + review_request with envelope and payload
//   asserted), the disagreement-without-HITL path (logs only, no routing),
//   transparent inheritance of the base LLM fallback on LLM error, the
//   AgreementThreshold knob (0.99 still treats hyphen-vs-space as identical
//   because the splitter ignores both, but a swapped token triggers
//   routing), and a direct unit test of tokenJaccard.
//
//   The rule comparator is invoked synthetically in-process: we hand the
//   identical Message that the agent itself received to the rule agent's
//   HandleMessage and decode the diagnosis_proposal it emits — no bus, no
//   second supervisor hop.
//
// Key declarations:
//   - newActiveLearningCaseStateJSON: CAP fixture builder
//   - TestActiveLearning_AgreementEmitsOnlyLLM
//   - TestActiveLearning_DisagreementWithHITLEmitsBothMessages
//   - TestActiveLearning_DisagreementWithoutHITLLogsOnly
//   - TestActiveLearning_LLMFailureFallsBack
//   - TestActiveLearning_AgreementThresholdRespected
//   - TestTokenJaccard_BasicCases
//
// See doc.go for package-level documentation.

package agents
