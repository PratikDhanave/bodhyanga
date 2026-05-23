// metrics_test.go — unit tests for case-level metric scoring.
//
// Overview:
//   Confirms ScoreCase reports a diagnosis match for a known-good case, that
//   cost utilization lands in the expected range when total cost is well
//   below the budget cap, and that RPM alert counts pass through correctly.
//
// Key declarations:
//   - TestScoreCase: end-to-end scoring of a representative CaseState.
//
// See doc.go for package-level documentation.

package eval
