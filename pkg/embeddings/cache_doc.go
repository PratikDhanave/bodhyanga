// cache.go — the Cache interface, CacheKey hashing, and the NewCached
// wrapper that turns a Cache + Embedder into a memoised Embedder.
//
// Overview:
//   Cache is the two-method interface (Get/Put for []float32) any backing
//   store can satisfy. CacheKey(provider, model, text) hashes the tuple
//   so the same input always maps to the same key. NewCached wraps an
//   Embedder so Embed first splits inputs into hits and misses, calls
//   the underlying Embedder only for the misses, and re-assembles the
//   result in the caller's original order — so a batch with some hits
//   and some misses only pays for the misses.
//
// Key declarations:
//   - Cache: Get/Put interface
//   - CacheKey: deterministic per-text key
//   - NewCached: wrap an Embedder with a Cache
//
// See doc.go for package-level documentation.

package embeddings
