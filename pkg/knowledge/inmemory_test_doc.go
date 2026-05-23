// inmemory_test.go — tests for the in-memory store, hybrid fusion, and corpus loader.
//
// Overview:
//   The tests use embeddings.Mock alongside a small pinnedEmbedder helper
//   that scripts canned vector batches per Embed call. That lets us pin
//   exactly which document is most similar to the query and verify ranking
//   without relying on a real embedder. Corpus tests round-trip a JSON file
//   through t.TempDir() and exercise the malformed-JSON error path.
//
// Key declarations:
//   - pinnedEmbedder: test-only Embedder that replays a scripted [][]float32 batch per call
//   - TestInMemoryIndexAndSearch
//   - TestInMemoryReplacesOnReIndex
//   - TestInMemorySizeReportsCount
//   - TestInMemorySearchWithNoIndexReturnsEmpty
//   - TestHybridFusesVectorAndBM25
//   - TestLoadCorpusFromTempFile
//   - TestLoadCorpusBadJSON
//
// See doc.go for package-level documentation.

package knowledge
