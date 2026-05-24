// main_test.go — unit tests for the SD-Bench fixture runner.
//
// Overview:
//   Exercises manifest loading (including manifest-over-file metadata
//   precedence), the repo's data/cases/manifest.json shape, per-provider
//   summary aggregation, regression detection in both directions
//   (accuracy floor + cost ceiling) with tolerance, baseline I/O round
//   tripping, and headline table formatting. The full diagnostic pipeline
//   is exercised indirectly by `go run ./cmd/bench` — these tests stay
//   bus-free so they run in well under a second.
//
// Key declarations:
//   - writeJSON / repoRoot / synthResult: per-test helpers.
//   - TestLoadManifest_* : manifest schema, overrides, error paths.
//   - TestRepoManifestLoads: smoke test against data/cases/manifest.json.
//   - TestSummarizeProvider_*: aggregation math (Pareto, under-budget).
//   - TestDetectRegressions_*: per-provider gate semantics + tolerance.
//   - TestFormatHeadlineTable_*: column / value coverage on the table.
//   - TestWriteBaseline_RoundTrip: baseline writer ↔ reader round trip.
//   - TestLoadCases_PrefersManifest / TestSplitCSV: small helpers.
//
// See doc.go for package-level documentation.

package main
