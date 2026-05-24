// main.go — SD-Bench style fixture runner (accuracy vs cost report with
// baseline regression gating).
//
// Overview:
//   Loads a manifest of case fixtures (-manifest, default
//   data/cases/manifest.json), drives each through a fresh diagnostic
//   pipeline headlessly for every requested provider (-providers, default
//   `rule`), captures the final CaseState via a bus subscription, scores
//   it with eval.ScoreCase, prints a per-provider markdown-style headline
//   table (accuracy, mean cost, mean steps, under-budget %, mean RPM
//   alerts, Pareto = accuracy/cost), and optionally fails the build when
//   any provider regresses below the values recorded in
//   data/cases/baseline.json.
//
// Key declarations:
//   - main: parses flags, loads manifest/cases, runs every provider × case,
//     prints the report, and runs the regression gate.
//   - fixture / manifestEntry / manifestFile: case input + manifest schema.
//   - result / providerRun / providerSummary: per-case + per-provider state.
//   - baselineEntry / baselineFile: persisted thresholds for regression
//     gating; checkRegressions / writeBaseline drive -fail-on-regression
//     and -update-baseline.
//   - runCase: spins up the pipeline and captures the terminal diagnosis
//     state via a done channel (no time.Sleep — bench scales to many cases).
//   - loadManifest / loadFixturesGlob / readFixtureFile: I/O helpers.
//   - summarizeProvider / formatHeadlineTable: aggregation + formatting,
//     exposed so tests can assert on them without spawning the full pipeline.
//
// See doc.go for package-level documentation.

package main
