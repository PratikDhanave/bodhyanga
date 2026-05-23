package embeddings

import (
	"container/list"
	"context"
	"sync"
)

// memoryCache is an in-memory embeddings.Cache with optional LRU
// eviction. Each entry stores a defensive copy of the []float32 vector
// so callers cannot mutate the cache by reslicing what Get returned.
//
// Concurrency: a single sync.Mutex guards the map and the linked list,
// since container/list is not goroutine-safe. Tests run under -race to
// confirm the locking is correct end-to-end.
type memoryCache struct {
	mu         sync.Mutex
	maxEntries int
	order      *list.List
	entries    map[string]*memoryEntry
}

type memoryEntry struct {
	vec  []float32
	node *list.Element
}

// NewMemoryCache returns a process-local Cache with LRU eviction when
// maxEntries > 0. A value <= 0 disables eviction (unbounded). Vectors
// are deep-copied on both Put and Get so the caller and the cache cannot
// reach into each other's storage.
func NewMemoryCache(maxEntries int) Cache {
	return &memoryCache{
		maxEntries: maxEntries,
		order:      list.New(),
		entries:    make(map[string]*memoryEntry),
	}
}

// Get implements Cache. On hit, promotes the key to the front of the LRU
// order and returns a copy of the cached vector.
func (c *memoryCache) Get(_ context.Context, key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e.node)
	return copyVec(e.vec), true
}

// Put implements Cache. Stores a copy of vec; evicts the least-recently
// used key when the map exceeds maxEntries.
func (c *memoryCache) Put(_ context.Context, key string, vec []float32) {
	if vec == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		e.vec = copyVec(vec)
		c.order.MoveToFront(e.node)
		return
	}
	node := c.order.PushFront(key)
	c.entries[key] = &memoryEntry{vec: copyVec(vec), node: node}
	if c.maxEntries > 0 && c.order.Len() > c.maxEntries {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.entries, oldest.Value.(string))
		}
	}
}

func copyVec(v []float32) []float32 {
	out := make([]float32, len(v))
	copy(out, v)
	return out
}
