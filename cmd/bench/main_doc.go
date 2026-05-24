// main.go — SD-Bench style fixture runner (accuracy vs cost report).
//
// Overview:
//   Loads every demo_*.json case fixture under -cases, drives each through
//   a fresh diagnostic pipeline headlessly, captures the final CaseState via
//   a bus subscription, scores it with eval.ScoreCase, and prints a table or
//   JSON Pareto-frontier report. Optionally runs an LLM-as-judge over every
//   case and gates the overall result against a configurable accuracy
//   threshold — designed to slot into CI as a regression gate.
//
// Key declarations:
//   - main: parses flags, loads fixtures, runs cases, grades, prints, gates.
//   - fixture / result / bench: case input, per-case result (with optional
//     JudgeGrade), summary aggregate.
//   - runCase: spins up the pipeline and captures the terminal diagnosis state.
//   - loadFixtures / printProviderTable / printComparison / summarize: I/O
//     and formatting helpers.
//   - buildJudgeFromFlag / newJudgeLLMClient: judge wiring; judge client is
//     wrapped with agents.LoadDefaultLLMCache so repeated bench re-runs are
//     free (honours BODH_LLM_CACHE_DIR).
//   - attachJudgeGrades: BatchGrade per provider, attaches Grade to results.
//   - judgePassFail / providerJudgeMean: gate logic, extracted for tests.
//
// See doc.go for package-level documentation.

package main
