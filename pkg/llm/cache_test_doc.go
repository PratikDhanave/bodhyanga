// cache_test.go — tests for the in-memory and file-backed Cache
// implementations, the CacheKey hash, and the NewCached wrapper.
//
// Overview:
//   Covers: round-trip Put/Get with deep-copy isolation, LRU eviction at
//   the configured cap, unbounded behaviour when max <= 0, CacheKey
//   stability and sensitivity to every field that should influence it
//   (temperature, message text, JSON schema, image bytes, model,
//   provider), the cached-client hit path (one mock call regardless of
//   how often we re-request), the error-not-cached invariant, and for
//   the file cache: round-trip, mtime-based LRU pruning, and survival
//   across a fresh constructor on the same directory.
//
// Key declarations:
//   - TestMemoryCache_PutGetRoundTrip
//   - TestMemoryCache_LRUEvictsOldest
//   - TestMemoryCache_UnboundedWhenMaxZero
//   - TestCacheKey_StableAcrossCalls
//   - TestCacheKey_VariesWithRequest
//   - TestCachedClient_HitsAvoidUnderlyingCall
//   - TestCachedClient_ErrorsNotCached
//   - TestFileCache_PutGetRoundTrip
//   - TestFileCache_LRUByMTime
//   - TestFileCache_SurvivesProcessRestart
//
// See doc.go for package-level documentation.

package llm
