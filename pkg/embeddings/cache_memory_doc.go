// cache_memory.go — in-memory embeddings.Cache with optional LRU
// eviction.
//
// Overview:
//   NewMemoryCache(maxEntries) returns a Cache backed by a map + a
//   doubly-linked-list LRU order. Get and Put are O(1); when entries
//   exceed maxEntries the oldest key is dropped. Vectors are deep-copied
//   on both Put and Get so callers cannot mutate the cache by reslicing
//   what Get returned. A single sync.Mutex guards the map and the list.
//
// Key declarations:
//   - NewMemoryCache(maxEntries int) Cache
//   - memoryCache, memoryEntry, copyVec: internal helpers
//
// See doc.go for package-level documentation.

package embeddings
