// cohort_test.go — unit tests for cohort-level analytics aggregations.
//
// Overview:
//   Validates the arithmetic in SummarizeCases (means, under-budget fraction)
//   over a small fixture of interaction records and confirms GapBreakdown
//   counts care gaps by rule from a PanelReport.
//
// Key declarations:
//   - TestSummarizeCases: covers mean accuracy, mean cost, and under-budget percentage.
//   - TestGapBreakdown: confirms per-rule gap histogram from a PanelReport.
//
// See doc.go for package-level documentation.

package analytics
