// diagnostician_selfconsistency.go — sample-N + vote diagnostician.
//
// Overview:
//   SelfConsistencyDiagnostician wraps an LLMDiagnostician and runs the
//   same diagnostic prompt N times in parallel with a higher sampling
//   temperature, then takes a majority vote across the resulting
//   proposals. Replaces single-call LLM variance with a more stable
//   consensus answer — Wang et al., "Self-Consistency Improves Chain of
//   Thought Reasoning" (2023). The agent's ID remains "diagnostician" so
//   supervisor routing is unchanged.
//
//   Behaviour:
//     - Builds the base llm.Request once via Base.buildRequest so the
//       prompt, JSON schema, RAG retrieval, and citation reconciliation
//       are shared across every sample.
//     - Overrides Temperature on the cloned request so samples can
//       actually disagree (default 0.6 vs the base's deterministic 0.2).
//     - Fans the N calls out under a semaphore-capped goroutine pool
//       (default max 4 in-flight) with a single shared wall-clock
//       budget (default 90s).
//     - Votes by normalised diagnosis label (lower / trim / collapse
//       whitespace). Tie-break by mean confidence, then by
//       lexicographic order for determinism.
//     - Aggregates: winning original Diagnosis string + Rationale +
//       Citations from the highest-confidence vote inside the winning
//       bucket; Confidence = mean of the winning bucket; Differential =
//       frequency-ordered union across all samples.
//     - If fewer than half the samples succeed, delegates to
//       Base.runFallback (rule-based agent).
//
// Key declarations:
//   - SelfConsistencyDiagnostician: struct with Base, Samples,
//     Temperature, Timeout, MaxConcurrency
//   - DefaultSelfConsistencySamples = 5
//   - maxSelfConsistencySamples = 10
//   - HandleMessage: agent entry point
//   - voteSelfConsistency / aggregateSelfConsistencyProposal /
//     unionDifferentials / normaliseDiagnosis: internal helpers
//   - cloneRequestForSample: per-goroutine request deep-copy
//
// See doc.go for package-level documentation.

package agents
