// cache_test.go — tests for in-memory and file-backed embeddings.Cache
// implementations and the NewCached wrapper.
//
// Overview:
//   Covers: round-trip Put/Get with caller/cache isolation, LRU eviction
//   at the configured cap, the cached-embedder hit path (one underlying
//   call regardless of how often we re-request the same batch), the
//   mixed hit/miss path that only calls the underlying Embedder for
//   misses but preserves the caller's original ordering in the result,
//   the file-cache round-trip, and survival across a fresh constructor
//   on the same directory.
//
// Key declarations:
//   - TestMemoryCache_PutGetRoundTrip
//   - TestMemoryCache_LRUEvictsOldest
//   - TestCachedEmbedder_HitsAvoidUnderlyingCall
//   - TestCachedEmbedder_MixedHitsAndMisses
//   - TestFileCache_PutGetRoundTrip
//   - TestFileCache_SurvivesProcessRestart
//
// See doc.go for package-level documentation.

package embeddings
