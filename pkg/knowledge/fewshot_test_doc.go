// fewshot_test.go — tests for LoadFewShotCorpus.
//
// Overview:
//   Exercises the HITL-decision loader across the four documented status
//   classes (approved, approved_with_modifications, rejected, pending) and
//   asserts that only the first two reach the resulting corpus. Covers both
//   accepted on-disk shapes (bare JSON array and {"reviews":[...]} wrapper),
//   malformed JSON, and empty input. The final test runs against the
//   committed data/hitl-sample.json so changes to the fixture are caught
//   immediately.
//
// Key declarations:
//   - TestLoadFewShotCorpus_OnlyApprovedAndModified
//   - TestLoadFewShotCorpus_HandlesArrayAndWrapperFormats
//   - TestLoadFewShotCorpus_BadJSONErrors
//   - TestLoadFewShotCorpus_EmptyFileReturnsEmpty
//   - TestLoadFewShotCorpus_OnSampleFixture
//
// See doc.go for package-level documentation.

package knowledge
