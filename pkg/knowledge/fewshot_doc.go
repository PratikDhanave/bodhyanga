// fewshot.go — HITL-decision loader for in-context few-shot retrieval.
//
// Overview:
//   LoadFewShotCorpus reads a JSON dump of medical.ReviewRequest records (the
//   same shape consumed by cmd/export-hitl-feedback — bare array or
//   {"reviews":[...]} wrapper) and emits one knowledge.Document per
//   clinician-approved case. Rejected and pending reviews are skipped so the
//   resulting corpus only injects positive examples into the LLM
//   diagnostician's prompt. Each Document's Content is a compact "Case: …
//   Approved diagnosis: …" block built from the review's case_state metadata
//   and either the approved payload or the modification text.
//
// Key declarations:
//   - LoadFewShotCorpus(path) ([]Document, error)
//   - documentFromReview(r): per-review Document constructor
//   - summariseCaseState(r): 2-3 line summary of the CaseState
//
// See doc.go for package-level documentation.

package knowledge
