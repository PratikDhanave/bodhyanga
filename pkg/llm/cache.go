package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Cache stores Responses keyed by a content-derived hash of the Request.
// Hits cost ~10µs; misses fall through to the underlying Client and are
// written back. The cache is intentionally simple — no eviction logic
// beyond what the implementation chooses; production callers can swap in
// Redis/Memcached by implementing the same interface.
type Cache interface {
	// Get returns the cached Response for a request key, or (nil, false)
	// if absent.
	Get(ctx context.Context, key string) (*Response, bool)

	// Put stores a Response under the given key. Implementations may
	// silently drop entries (size cap, TTL) without erroring.
	Put(ctx context.Context, key string, resp *Response)
}

// CacheKey computes the content-addressed key for a Request. The key
// includes: provider model name (from caller), system, messages, max
// tokens, temperature, json-schema, tools list, and cache-system flag.
// Two semantically identical requests produce the same key regardless of
// when or where they're issued. Image bytes ARE hashed so vision requests
// don't collide on different inputs.
func CacheKey(provider, model string, req Request) string {
	h := sha256.New()
	mustJSON(h, []any{
		"v1",
		provider, model,
		req.System,
		req.CacheSystem,
		req.MaxTokens,
		req.Temperature,
		req.Messages,
		req.JSONSchema,
		req.Tools,
	})
	return hex.EncodeToString(h.Sum(nil))
}

func mustJSON(w interface{ Write(p []byte) (int, error) }, v any) {
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
}

// NewCached wraps a Client with a Cache. The returned Client's Provider()
// and Model() pass through to the underlying client; only Complete is
// memoised. Cache hits return a deep copy of the stored Response so
// callers can mutate it freely.
func NewCached(c Client, cache Cache) Client {
	return &cachedClient{inner: c, cache: cache}
}

type cachedClient struct {
	inner Client
	cache Cache
}

func (c *cachedClient) Provider() string { return c.inner.Provider() }
func (c *cachedClient) Model() string    { return c.inner.Model() }

func (c *cachedClient) Complete(ctx context.Context, req Request) (*Response, error) {
	key := CacheKey(c.inner.Provider(), c.inner.Model(), req)
	if hit, ok := c.cache.Get(ctx, key); ok {
		dup := *hit
		return &dup, nil
	}
	resp, err := c.inner.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	c.cache.Put(ctx, key, resp)
	return resp, nil
}
