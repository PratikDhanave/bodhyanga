// jsonstore_test.go — unit tests for the JSON-file persistence store.
//
// Overview:
//   Exercises the round-trip behavior of FileStore: save an interaction record,
//   set a key-value pair, then reopen the same path and confirm both pieces
//   of state are restored from disk.
//
// Key declarations:
//   - TestFileStoreRoundTrip: validates persistence of records and KV entries across an Open/reopen cycle.
//
// See doc.go for package-level documentation.

package persistence
