// main.go — SD-Bench style fixture runner (accuracy vs cost report).
//
// Overview:
//   Loads every demo_*.json case fixture under -cases, drives each through
//   a fresh diagnostic pipeline headlessly, captures the final CaseState via
//   a bus subscription, scores it with eval.ScoreCase, and prints a table or
//   JSON Pareto-frontier report.
//
// Key declarations:
//   - main: parses flags, loads fixtures, runs cases, prints the report.
//   - fixture / result / bench: case input, per-case result, summary aggregate.
//   - runCase: spins up the pipeline and captures the terminal diagnosis state.
//   - loadFixtures / printTable / summarize: I/O and formatting helpers.
//
// See doc.go for package-level documentation.

package main
