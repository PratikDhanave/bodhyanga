package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// Cache stores embedding vectors keyed by (provider, model, text). It
// makes cold-starts cheap: the 15-document corpus is embedded once on
// first boot, then re-loads from disk on every subsequent boot.
type Cache interface {
	// Get returns the cached vector for a key, or (nil, false) if absent.
	Get(ctx context.Context, key string) ([]float32, bool)

	// Put stores a vector under the given key. Implementations may drop
	// entries silently (size cap, TTL).
	Put(ctx context.Context, key string, vec []float32)
}

// CacheKey computes the cache key for one text input.
func CacheKey(provider, model, text string) string {
	h := sha256.New()
	h.Write([]byte(provider))
	h.Write([]byte{0})
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

// NewCached wraps an Embedder with a Cache. Embed splits inputs into hit
// vs. miss subsets, calls the underlying Embedder only for misses, then
// re-assembles the result in the caller's original order.
func NewCached(e Embedder, cache Cache) Embedder {
	return &cachedEmbedder{inner: e, cache: cache}
}

type cachedEmbedder struct {
	inner Embedder
	cache Cache
}

func (c *cachedEmbedder) Provider() string { return c.inner.Provider() }
func (c *cachedEmbedder) Model() string    { return c.inner.Model() }
func (c *cachedEmbedder) Dimension() int   { return c.inner.Dimension() }

func (c *cachedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrEmptyInput
	}
	out := make([][]float32, len(texts))
	misses := make([]string, 0)
	missIdx := make([]int, 0)
	keys := make([]string, len(texts))
	for i, t := range texts {
		k := CacheKey(c.inner.Provider(), c.inner.Model(), t)
		keys[i] = k
		if vec, ok := c.cache.Get(ctx, k); ok {
			out[i] = vec
			continue
		}
		misses = append(misses, t)
		missIdx = append(missIdx, i)
	}
	if len(misses) == 0 {
		return out, nil
	}
	vecs, err := c.inner.Embed(ctx, misses)
	if err != nil {
		return nil, err
	}
	for j, vec := range vecs {
		idx := missIdx[j]
		out[idx] = vec
		c.cache.Put(ctx, keys[idx], vec)
	}
	return out, nil
}

