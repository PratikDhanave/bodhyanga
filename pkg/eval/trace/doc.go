// Package trace provides exploratory data analysis (EDA) tooling for
// production traces emitted by multi-agent pipelines.
//
// Inspired by Chapter 11 of "Evals for AI Engineers," this package gives
// operators the building blocks to load, filter, aggregate, and report on
// traces at scale without reaching for an external notebook. The primary
// entry point is LoadTraces, which reads a JSONL file into a []Trace slice
// that every other function in the package consumes.
//
// Key capabilities:
//
//   - Percentile latency stats (p50/p95/p99)
//   - Outlier detection via IQR or percentile methods
//   - Cross-tabulation of any metadata dimension against a numeric metric
//   - TF-IDF-like semantic search over queries and outputs
//   - CSV/JSONL export for external review workflows
//   - Human-readable Markdown summary reports
package trace
