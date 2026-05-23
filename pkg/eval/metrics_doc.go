// metrics.go — case-level scoring (SD Bench style) plus RPM-aware proxies.
//
// Overview:
//   Computes per-case metrics — diagnosis match, accuracy, cost utilization,
//   message step count, RPM alert count, under-budget flag — from a completed
//   medical.CaseState, and adapts them into an eval.InteractionRecord that
//   downstream stores and analytics packages consume.
//
// Key declarations:
//   - CaseMetrics: scored outcomes for a single completed case.
//   - ScoreCase: derive CaseMetrics from a medical.CaseState plus message-step count.
//   - CaseMetrics.ToInteractionRecord: project metrics into the eval store schema.
//
// See doc.go for package-level documentation.

package eval
