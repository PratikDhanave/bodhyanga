// cache.go — the Cache interface, CacheKey hashing, and the NewCached
// wrapper that memoises Client.Complete.
//
// Overview:
//   Cache is the small two-method interface (Get/Put) that any backing
//   store can satisfy. CacheKey(provider, model, req) hashes everything
//   semantically meaningful about a Request — system prompt, messages
//   (including any image bytes), max tokens, temperature, JSON schema,
//   tools, and the cache-system flag — into a SHA-256 hex string so the
//   same request always maps to the same key. NewCached(client, cache)
//   returns a Client that consults the cache before calling through.
//
// Key declarations:
//   - Cache: Get/Put interface
//   - CacheKey: deterministic content-addressed key
//   - NewCached: wrap a Client with a Cache
//
// See doc.go for package-level documentation.

package llm
