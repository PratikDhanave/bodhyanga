// diagnostician_selfconsistency_test.go — unit tests for
// SelfConsistencyDiagnostician.
//
// Overview:
//   Exercises the sample-N + vote diagnostician across six paths:
//   the happy-path majority vote, the equal-count tie-break by mean
//   confidence, the majority-errors quorum fallback, the
//   frequency-ordered differential union, citation propagation from
//   the winning vote, and the DefaultSelfConsistencySamples=5 default.
//
//   Reuses scriptedClient (defined in diagnostician_debate_test.go) so
//   each sample gets a distinct canned response. Tests run with
//   MaxConcurrency=1 so the order of replies handed out matches the
//   order they are written — the voting logic itself is independent of
//   scheduling but deterministic dispatch makes the tests easier to
//   reason about.
//
// Key declarations:
//   - newSelfConsistencyScript: convenience scriptedClient builder
//   - TestSelfConsistency_VotesAcrossSamples
//   - TestSelfConsistency_TiebreakByMeanConfidence
//   - TestSelfConsistency_FallsBackOnMajorityErrors
//   - TestSelfConsistency_AggregatesDifferentials
//   - TestSelfConsistency_PreservesCitations
//   - TestSelfConsistency_DefaultSamplesIs5
//
// See doc.go for package-level documentation.

package agents
