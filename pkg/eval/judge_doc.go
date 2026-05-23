// judge.go — LLM-as-judge for grading diagnostician outputs.
//
// Overview:
//   Wraps an llm.Client behind a Judge interface that scores a candidate
//   diagnosis against a gold-standard reference using a clinical rubric
//   (correctness, specificity, safety, differential_quality). The judge
//   sends a structured-output request whose JSON Schema requires score,
//   pass, rationale, and the four dimensional sub-scores, then clamps the
//   overall score into [0,1] and derives Pass = score >= 0.6. A cached
//   system prompt holds the rubric so re-grading runs reuse the prefix.
//
// Key declarations:
//   - Judge: interface that grades one GradeItem.
//   - GradeItem: scenario label, expected/actual strings, optional rubric
//     override, and free-form context handed to the judge.
//   - Grade: numeric score, pass flag, rationale, dimensional sub-scores,
//     and the provider/model that produced the verdict.
//   - JudgeSystemPrompt: package-visible const describing the rubric.
//   - PassThreshold: minimum overall score to count as a pass (0.6).
//   - NewLLMJudge: constructor returning a Judge backed by llm.Client.
//   - BatchGrade: fan out Grade across a pool of 4 workers.
//
// See doc.go for package-level documentation.

package eval
