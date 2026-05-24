// main_test.go — tests for cmd/bench: flag parsing, pipeline smoke, summary
// math, and the LLM-as-judge regression gate.
//
// Overview:
//   These tests exercise the bench runner's pure helpers (splitCSV,
//   summarize, judgePassFail, providerJudgeMean) and the judge wiring
//   end-to-end against an in-memory llm.Mock. The judge-gate logic and
//   score aggregation are extracted into helper functions so tests can
//   call them without booting the full pipeline. One smoke test drives
//   the real diagnostic pipeline through runCase for the CAP fixture so
//   we catch regressions in the pipeline wiring itself.
//
// Tests:
//   - TestSplitCSV_BasicAndEmpty
//   - TestRunCaseWithRuleEmitsFinalState
//   - TestSummarize_EmptyAndPopulated
//   - TestJudgeGate_FailsBelowThreshold
//   - TestJudgeGate_PassesAtOrAboveThreshold
//   - TestJudgeGate_SkipsProvidersWithNoGrades
//   - TestBuildJudgeFromFlag_UnknownProviderReturnsError
//   - TestAttachJudgeGrades_RoutesThroughJudgeInterface
//   - TestAttachJudgeGrades_JudgeErrorLeavesGradeNil
//
// See doc.go for package-level documentation.

package main
