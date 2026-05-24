// diagnostician_active.go — active-learning diagnostician.
//
// Overview:
//   ActiveLearningDiagnostician composes an LLMDiagnostician (primary)
//   with a rule-based DiagnosticianAgent (comparator). Every case is
//   diagnosed twice — once by each path — and the two diagnosis strings
//   are compared via token-Jaccard similarity over the lowercased,
//   alphanumeric-split tokens of each label. When the score falls below
//   AgreementThreshold (default 0.6) the case is treated as a
//   disagreement.
//
//   Behaviour:
//     - Agree: emit ONLY the LLM's diagnosis_proposal to
//       reasoning_verifier (identical to the base LLMDiagnostician).
//     - Disagree + HITL on: emit the LLM proposal toward
//       reasoning_verifier (so the pipeline continues) AND emit a
//       review_request to human_review with Kind
//       "active_learning_disagreement", Priority "normal", a 24-hour
//       SLA, and a structured Payload of {llm, rule, case_state,
//       jaccard, agreement_threshold} ready for the
//       cmd/export-hitl-feedback exporter to ship as labelled training
//       data. NextTo/NextType point at engagement /
//       "active_learning_logged" because the review's purpose is the
//       clinician's label, not pipeline resumption — engagement no-ops
//       on this type.
//     - Disagree + HITL off: log "disagreement (NOT routed — HITL off)"
//       and emit only the LLM proposal. Useful for shadow / dry-run
//       rollouts.
//     - LLM error: the LLMDiagnostician's existing fallback engages
//       transparently — active learning inherits the behaviour, sees
//       the rule-based proposal on both sides, and emits only that.
//     - Defensive: if either proposal fails to decode, the agent
//       emits the LLM output as-is and logs a warning rather than
//       crashing.
//
// Key declarations:
//   - ActiveLearningDiagnostician: struct with LLM, Rule, HITL,
//     AgreementThreshold
//   - defaultActiveLearningAgreementThreshold = 0.6
//   - activeLearningReviewKind, activeLearningNextType,
//     activeLearningSLAMinutes: HITL envelope constants
//   - HandleMessage: agent entry point
//   - extractActiveLearningProposal: decode helper over an emitted
//     message slice
//   - buildReviewMessage / formatActiveLearningDetail: HITL packaging
//   - tokenJaccard / tokenSet: pure helpers used for label similarity
//
// See doc.go for package-level documentation.

package agents
