// cache_file.go — disk-backed Cache, one JSON file per entry.
//
// Overview:
//   NewFileCache(dir, maxEntries) returns a Cache that persists each
//   Response to <dir>/<key>.json. Writes are atomic — bytes go through a
//   temp file in the same directory and are renamed into place, so
//   concurrent readers never observe a torn write. Reads decode the JSON
//   on every Get; the OS page cache keeps warm files effectively
//   in-memory. LRU pruning runs on Put: when the file count exceeds
//   maxEntries the oldest entries by mtime are deleted until the
//   directory is back under the cap. maxEntries <= 0 disables pruning.
//   A single sync.Mutex serialises writes so the prune step does not
//   race with another writer.
//
// Key declarations:
//   - NewFileCache(dir string, maxEntries int) (Cache, error)
//   - fileCache: internal implementation
//
// See doc.go for package-level documentation.

package llm
