package llm

import (
	"container/list"
	"context"
	"sync"
)

// memoryCache is an in-memory Cache with optional LRU eviction.
//
// The Get path is O(1): a hash map lookup and a doubly-linked-list move to
// the front. Put is also O(1) amortised — when len exceeds maxEntries the
// oldest key is dropped. maxEntries <= 0 disables eviction (unbounded).
//
// Concurrency: all reads and writes are guarded by a single sync.Mutex.
// container/list is not goroutine-safe, so the mutex must wrap every
// touch of the linked list, not just the map. Tests run under -race so
// the locking is exercised end-to-end.
type memoryCache struct {
	mu         sync.Mutex
	maxEntries int
	order      *list.List
	entries    map[string]*memoryEntry
}

type memoryEntry struct {
	resp *Response
	node *list.Element
}

// NewMemoryCache returns a process-local Cache with LRU eviction when
// maxEntries > 0. A value <= 0 disables eviction (unbounded). The cache
// stores a deep copy of every Response on Put and returns a deep copy on
// Get so callers can mutate either side without corrupting the other.
func NewMemoryCache(maxEntries int) Cache {
	return &memoryCache{
		maxEntries: maxEntries,
		order:      list.New(),
		entries:    make(map[string]*memoryEntry),
	}
}

// Get implements Cache. On hit, the key is promoted to the front of the
// LRU order list before the cached Response is copied and returned.
func (c *memoryCache) Get(_ context.Context, key string) (*Response, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e.node)
	return copyResponse(e.resp), true
}

// Put implements Cache. A copy of resp is stored so later mutations by the
// caller cannot poison the cache. If maxEntries > 0 and inserting this key
// would exceed it, the least-recently-used entry is evicted.
func (c *memoryCache) Put(_ context.Context, key string, resp *Response) {
	if resp == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		e.resp = copyResponse(resp)
		c.order.MoveToFront(e.node)
		return
	}
	node := c.order.PushFront(key)
	c.entries[key] = &memoryEntry{resp: copyResponse(resp), node: node}
	if c.maxEntries > 0 && c.order.Len() > c.maxEntries {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.entries, oldest.Value.(string))
		}
	}
}

// copyResponse returns a shallow-with-slice-copy of r so the caller and
// the cache cannot reach into each other's storage. The ToolCalls slice is
// independently allocated; the json.RawMessage inside each ToolCall is
// itself a byte slice — we copy it so a caller mutating bytes does not
// corrupt the cached entry. Provider/Model/Content are strings (immutable
// in Go) so a struct copy suffices for them.
func copyResponse(r *Response) *Response {
	if r == nil {
		return nil
	}
	out := *r
	if len(r.ToolCalls) > 0 {
		out.ToolCalls = make([]ToolCall, len(r.ToolCalls))
		for i, tc := range r.ToolCalls {
			out.ToolCalls[i] = ToolCall{Name: tc.Name}
			if tc.Arguments != nil {
				args := make([]byte, len(tc.Arguments))
				copy(args, tc.Arguments)
				out.ToolCalls[i].Arguments = args
			}
		}
	}
	return &out
}
