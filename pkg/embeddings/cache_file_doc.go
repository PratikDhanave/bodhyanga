// cache_file.go — disk-backed embeddings.Cache, one binary file per
// vector.
//
// Overview:
//   NewFileCache(dir, maxEntries) persists each vector to <dir>/<key>.vec
//   in a tight binary layout: 4-byte little-endian uint32 length prefix,
//   followed by length * 4 bytes of little-endian float32 values. Binary
//   was chosen over JSON for ~6x smaller files (a 1536-dim vector is
//   ~6KB binary vs ~30KB JSON) and faster decode. Writes are atomic via
//   a temp file + rename. LRU pruning on Put deletes oldest-by-mtime
//   entries when the file count exceeds maxEntries. maxEntries <= 0
//   disables pruning. A single sync.Mutex serialises writes.
//
// Key declarations:
//   - NewFileCache(dir string, maxEntries int) (Cache, error)
//   - fileCache, encodeVec, decodeVec: internal implementation
//
// See doc.go for package-level documentation.

package embeddings
