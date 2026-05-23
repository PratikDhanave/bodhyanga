// differential_ranker_test.go — unit tests for DifferentialRanker.
//
// Overview:
//   Drives DifferentialRanker against llm.Mock to verify the happy
//   path (new order matches the canned mock array), three graceful
//   degradation paths (LLM transport error, malformed JSON, empty
//   array — each returns the original Differential with nil error),
//   and that the LLM-emitted form of each entry is preserved verbatim
//   (so case differences between caller input and model output are
//   respected). Also checks that Rerank does not mutate the input
//   proposal — compose, don't modify.
//
// Key declarations:
//   - TestDifferentialRanker_HappyPath
//   - TestDifferentialRanker_LLMErrorReturnsOriginal
//   - TestDifferentialRanker_BadJSONReturnsOriginal
//   - TestDifferentialRanker_EmptyArrayReturnsOriginal
//   - TestDifferentialRanker_PreservesCaseInsensitiveEntries
//
// See doc.go for package-level documentation.

package agents
