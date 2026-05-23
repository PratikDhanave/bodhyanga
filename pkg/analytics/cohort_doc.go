// cohort.go — cohort-level aggregations of eval records and panel reports.
//
// Overview:
//   Rolls a slice of eval.InteractionRecord values into a CohortMetrics
//   summary (mean accuracy, mean cost, under-budget rate, RPM alert rate)
//   for outcome dashboards, and breaks down a medical.PanelReport into a
//   per-rule gap histogram.
//
// Key declarations:
//   - CohortMetrics: aggregated stats over a set of recorded cases.
//   - SummarizeCases: produce CohortMetrics from interaction records.
//   - GapBreakdown: count care gaps in a PanelReport by rule.
//
// See doc.go for package-level documentation.

package analytics
