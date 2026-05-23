// jsonstore.go — JSON-file backed store implementing eval.Store and memory.KeyValueStore.
//
// Overview:
//   FileStore persists evaluation interaction records and a flat key-value map
//   under a single JSON file so demo runs survive process restarts without
//   pulling in a database. State is loaded on Open and flushed on every write.
//
// Key declarations:
//   - FileStore: mutex-guarded JSON-on-disk store.
//   - Open: load (or create) a FileStore at a given path.
//   - FileStore.Save / FileStore.List: eval.Store implementation for InteractionRecords.
//   - FileStore.Get / FileStore.Set / FileStore.Delete: memory.KeyValueStore implementation.
//
// See doc.go for package-level documentation.

package persistence
