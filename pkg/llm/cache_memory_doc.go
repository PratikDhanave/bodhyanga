// cache_memory.go — in-memory Cache with optional LRU eviction.
//
// Overview:
//   NewMemoryCache(maxEntries) returns a Cache backed by a map + a
//   doubly-linked-list LRU order. Get is O(1): look up the key, promote
//   to MRU, return a deep copy of the cached Response. Put is also O(1)
//   amortised; when the entry count exceeds maxEntries the oldest key
//   is dropped. maxEntries <= 0 disables eviction (unbounded). A single
//   sync.Mutex guards both the map and the linked list — container/list
//   is not goroutine-safe on its own.
//
// Key declarations:
//   - NewMemoryCache(maxEntries int) Cache
//   - memoryCache, memoryEntry, copyResponse: internal helpers
//
// See doc.go for package-level documentation.

package llm
