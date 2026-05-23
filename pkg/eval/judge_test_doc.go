// judge_test.go — unit tests for the LLM-as-judge wrapper.
//
// Overview:
//   Drives NewLLMJudge with the llm.Mock client to confirm the happy-path
//   JSON is propagated into Grade, that Pass is recomputed from the clamped
//   numeric score (model can't lie its way to a pass), that LLM errors and
//   invalid JSON bubble back to the caller, and that out-of-range scores
//   (both above 1 and below 0) are clamped on both the overall score and
//   each dimensional sub-score. Also asserts the request sets
//   CacheSystem=true and uses the clinical_grade JSON schema, and that a
//   per-item Rubric overrides the cached default. BatchGrade tests confirm
//   the worker pool fans out (>= 2 in flight, <= 4) and handles empty input.
//
// Key declarations:
//   - TestLLMJudge_HappyPath: end-to-end propagation of score, rationale,
//     dimensional sub-scores, and provider/model.
//   - TestLLMJudge_FailingScoreSetsPassFalse: derived Pass overrides the
//     model's optimistic boolean when score < 0.6.
//   - TestLLMJudge_LLMErrorBubblesUp: llm.Client errors surface wrapped.
//   - TestLLMJudge_InvalidJSONReturnsError: non-JSON content errors out.
//   - TestLLMJudge_ClampsScoreToZeroOneRange: > 1 and < 0 are clamped.
//   - TestLLMJudge_UsesCachedSystemPrompt: CacheSystem flag + schema name.
//   - TestLLMJudge_RubricOverride: per-item Rubric replaces the default.
//   - TestBatchGrade_RunsConcurrentlyAcrossItems: pool fans out to <= 4.
//   - TestBatchGrade_EmptyInput: graceful no-op for empty slices.
//
// See doc.go for package-level documentation.

package eval
