package llm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// fileCache persists each Response to a single JSON file at
// <dir>/<key>.json. Reads do not hit any in-memory index — each Get reads
// from disk and decodes the JSON. The OS page cache makes warm reads
// effectively in-memory; cold reads cost one syscall + tiny decode.
//
// LRU pruning happens on Put: when the file count exceeds maxEntries the
// oldest entries by mtime are deleted until the directory is back under
// the cap. maxEntries <= 0 disables pruning (unbounded).
//
// Atomicity: writes go through a temp file in the same directory and are
// renamed into place. The single-rename guarantee on POSIX (and on
// Windows, see os.Rename docs) prevents readers from observing a
// half-written entry. Get returns (nil, false) for any decode error so a
// corrupted file (e.g. partial pre-rename write from a previous crash)
// does not break the cache.
//
// Concurrency: a single sync.Mutex serialises Put operations so two
// concurrent writers cannot race on the prune step. Get is also locked —
// not strictly required for correctness of independent reads, but cheap
// and makes the implementation robust against the platform-specific
// behaviour of rename on Windows.
type fileCache struct {
	mu         sync.Mutex
	dir        string
	maxEntries int
}

// NewFileCache returns a disk-backed Cache rooted at dir. The directory is
// mkdir -p'd in the constructor. Each entry is one JSON file named after
// its key with a ".json" suffix. When maxEntries > 0 the cache prunes the
// oldest entries by mtime on every Put so the count stays at the cap.
func NewFileCache(dir string, maxEntries int) (Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &fileCache{dir: dir, maxEntries: maxEntries}, nil
}

// Get implements Cache. Returns (nil, false) when the file does not exist
// or when the JSON fails to decode (treated as a miss rather than a hard
// error — the caller will re-populate via the underlying client).
func (c *fileCache) Get(_ context.Context, key string) (*Response, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := filepath.Join(c.dir, key+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var resp Response
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, false
	}
	return &resp, true
}

// Put implements Cache. The Response is JSON-encoded, written to a temp
// file in the cache directory, fsynced into a final name, and then
// pruning runs (if maxEntries > 0). Errors are silently swallowed — the
// Cache interface documents that Put never returns; on disk failure the
// caller still gets the underlying client's answer, just without caching.
func (c *fileCache) Put(_ context.Context, key string, resp *Response) {
	if resp == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	final := filepath.Join(c.dir, key+".json")
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
		if filepath.Ext(name) != ".json" {
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
