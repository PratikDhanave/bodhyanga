package embeddings

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// fileCache persists each embedding vector to a single binary file at
// <dir>/<key>.vec. The on-disk layout is:
//
//	bytes 0..3       little-endian uint32 N (vector length)
//	bytes 4..4+4*N   N little-endian float32 values
//
// Binary was chosen over JSON for two reasons: a 1536-dim embedding is
// ~6KB binary vs ~30KB JSON, and decode is a single read + binary
// conversion instead of a JSON parse. Reads do not hit any in-memory
// index — each Get reads from disk; the OS page cache keeps warm reads
// effectively in-memory.
//
// LRU pruning runs on every Put: when the file count exceeds maxEntries
// the oldest entries by mtime are deleted until the directory is back
// under the cap. maxEntries <= 0 disables pruning (unbounded).
//
// Writes are atomic: bytes go into a temp file in the same directory and
// are renamed into place. A reader that races with a writer either sees
// the previous full version of the file or the new one — never a torn
// write. Get returns (nil, false) on any decode error so a stray
// half-written file (e.g. from a crash before atomic rename) cannot
// poison the cache.
//
// Concurrency: a single sync.Mutex serialises Put so two concurrent
// writers cannot race on the prune step. Get is also locked — not
// strictly required, but cheap and keeps semantics consistent across
// platforms.
type fileCache struct {
	mu         sync.Mutex
	dir        string
	maxEntries int
}

// NewFileCache returns a disk-backed Cache rooted at dir. The directory
// is mkdir -p'd in the constructor. Each entry is one binary .vec file.
// When maxEntries > 0 the cache prunes oldest-by-mtime on every Put.
func NewFileCache(dir string, maxEntries int) (Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &fileCache{dir: dir, maxEntries: maxEntries}, nil
}

// Get implements Cache. Returns (nil, false) when the file does not exist
// or when the binary payload is malformed (treated as a miss so the
// caller can re-embed).
func (c *fileCache) Get(_ context.Context, key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := filepath.Join(c.dir, key+".vec")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return decodeVec(b)
}

// Put implements Cache. Encodes vec, writes to a temp file, and atomically
// renames it into place; then runs LRU pruning. All errors are swallowed
// — Cache.Put is documented as best-effort.
func (c *fileCache) Put(_ context.Context, key string, vec []float32) {
	if vec == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b := encodeVec(vec)
	final := filepath.Join(c.dir, key+".vec")
	tmp, err := os.CreateTemp(c.dir, "cache-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, werr := tmp.Write(b); werr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if cerr := tmp.Close(); cerr != nil {
		_ = os.Remove(tmpName)
		return
	}
	if rerr := os.Rename(tmpName, final); rerr != nil {
		_ = os.Remove(tmpName)
		return
	}
	c.prune()
}

// encodeVec writes the 4-byte length prefix followed by little-endian
// float32 values.
func encodeVec(v []float32) []byte {
	out := make([]byte, 4+4*len(v))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(v)))
	for i, f := range v {
		binary.LittleEndian.PutUint32(out[4+4*i:4+4*i+4], math.Float32bits(f))
	}
	return out
}

// decodeVec inverts encodeVec. Returns (nil, false) on any malformation:
// too short, length prefix doesn't match payload bytes, etc.
func decodeVec(b []byte) ([]float32, bool) {
	if len(b) < 4 {
		return nil, false
	}
	n := binary.LittleEndian.Uint32(b[0:4])
	if len(b) != int(4+4*n) {
		return nil, false
	}
	out := make([]float32, n)
	for i := uint32(0); i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4+4*i : 4+4*i+4]))
	}
	return out, true
}

// prune deletes oldest-mtime entries until the directory holds at most
// maxEntries cache files. Called under c.mu.
func (c *fileCache) prune() {
	if c.maxEntries <= 0 {
		return
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type fileInfo struct {
		name  string
		mtime int64
	}
	files := make([]fileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".vec" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{name: name, mtime: info.ModTime().UnixNano()})
	}
	if len(files) <= c.maxEntries {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime < files[j].mtime })
	excess := len(files) - c.maxEntries
	for i := 0; i < excess; i++ {
		_ = os.Remove(filepath.Join(c.dir, files[i].name))
	}
}
