// memory.go — minimal key-value store interface and in-memory implementation.
//
// Overview:
//   Provides the small KeyValueStore abstraction used by agents for scratchpad
//   and conversation state, plus an InMemoryKV reference implementation that
//   is concurrency-safe and suitable for tests, demos, and local runs.
//
// Key declarations:
//   - KeyValueStore: Get / Set / Delete interface that pluggable backends satisfy.
//   - InMemoryKV: RWMutex-guarded map-backed store.
//   - NewInMemoryKV: constructor returning an empty thread-safe store.
//
// See doc.go for package-level documentation.

package memory
