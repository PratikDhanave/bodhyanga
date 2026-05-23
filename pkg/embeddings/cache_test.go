package embeddings

import (
	"context"
	"testing"
)

func TestMemoryCache_PutGetRoundTrip(t *testing.T) {
	c := NewMemoryCache(10)
	ctx := context.Background()
	want := []float32{0.1, 0.2, 0.3}
	c.Put(ctx, "k1", want)

	got, ok := c.Get(ctx, "k1")
	if !ok {
		t.Fatalf("expected hit")
	}
	if len(got) != 3 || got[0] != 0.1 || got[1] != 0.2 || got[2] != 0.3 {
		t.Fatalf("round-trip lost data: %+v", got)
	}

	// Mutate the returned vec; cached entry must be untouched.
	got[0] = 99
	got2, _ := c.Get(ctx, "k1")
	if got2[0] != 0.1 {
		t.Fatalf("cache poisoned by caller mutation: %v", got2)
	}
}

func TestMemoryCache_LRUEvictsOldest(t *testing.T) {
	c := NewMemoryCache(3)
	ctx := context.Background()
	c.Put(ctx, "a", []float32{1})
	c.Put(ctx, "b", []float32{2})
	c.Put(ctx, "c", []float32{3})
	// Touch "a" so "b" becomes oldest.
	if _, ok := c.Get(ctx, "a"); !ok {
		t.Fatalf("a missing")
	}
	c.Put(ctx, "d", []float32{4})

	if _, ok := c.Get(ctx, "b"); ok {
		t.Fatalf("expected b to be evicted")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := c.Get(ctx, k); !ok {
			t.Fatalf("expected %s to remain", k)
		}
	}
}

func TestCachedEmbedder_HitsAvoidUnderlyingCall(t *testing.T) {
	mock := &Mock{Dim: 4}
	cached := NewCached(mock, NewMemoryCache(16))
	ctx := context.Background()

	texts := []string{"alpha", "beta", "gamma"}
	first, err := cached.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("first Embed: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("first len: %d", len(first))
	}
	second, err := cached.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("second Embed: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("second len: %d", len(second))
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected exactly 1 underlying call, got %d", len(mock.Calls))
	}
}

func TestCachedEmbedder_MixedHitsAndMisses(t *testing.T) {
	mock := &Mock{Dim: 4}
	cache := NewMemoryCache(16)
	cached := NewCached(mock, cache)
	ctx := context.Background()

	// Warm the cache for "a" and "c" by embedding them first.
	if _, err := cached.Embed(ctx, []string{"a", "c"}); err != nil {
		t.Fatalf("warm: %v", err)
	}
	if len(mock.Calls) != 1 || len(mock.Calls[0]) != 2 {
		t.Fatalf("warm call: %+v", mock.Calls)
	}

	// Now embed 5 texts where "a" and "c" are hits, the other 3 are misses.
	texts := []string{"a", "b", "c", "d", "e"}
	out, err := cached.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("mixed: %v", err)
	}
	if len(out) != 5 {
		t.Fatalf("len: %d", len(out))
	}
	for i, v := range out {
		if len(v) == 0 {
			t.Fatalf("out[%d] empty", i)
		}
	}
	if len(mock.Calls) != 2 {
		t.Fatalf("expected 2 underlying calls total, got %d", len(mock.Calls))
	}
	missBatch := mock.Calls[1]
	if len(missBatch) != 3 {
		t.Fatalf("expected miss batch size 3, got %d (%v)", len(missBatch), missBatch)
	}
	// Caller's ordering — texts[1], texts[3], texts[4] — must be preserved.
	if missBatch[0] != "b" || missBatch[1] != "d" || missBatch[2] != "e" {
		t.Fatalf("miss batch order: %v", missBatch)
	}

	// Caller's result ordering is preserved: position-0 vector comes from
	// the cache, position-1 from the new call, etc. The Mock's default
	// pseudo-embedding puts the input index into vec[0], so we can verify
	// ordering by mapping back to that.
	// First call produced: a -> vec[0]=0, c -> vec[0]=1.
	// Second call produced: b -> vec[0]=0, d -> vec[0]=1, e -> vec[0]=2.
	want0 := []float32{0, 0, 1, 1, 2}
	for i, v := range out {
		if v[0] != want0[i] {
			t.Fatalf("out[%d][0] = %v, want %v (texts[%d]=%q)", i, v[0], want0[i], i, texts[i])
		}
	}
}

func TestFileCache_PutGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, err := NewFileCache(dir, 10)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	ctx := context.Background()
	want := []float32{0.0, -1.5, 3.14, 1e-7}
	c.Put(ctx, "k1", want)

	got, ok := c.Get(ctx, "k1")
	if !ok {
		t.Fatalf("expected hit")
	}
	if len(got) != len(want) {
		t.Fatalf("len: %d", len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestFileCache_SurvivesProcessRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	{
		c, err := NewFileCache(dir, 10)
		if err != nil {
			t.Fatalf("NewFileCache: %v", err)
		}
		c.Put(ctx, "persist", []float32{1.25, 2.5, 4.75})
	}
	c2, err := NewFileCache(dir, 10)
	if err != nil {
		t.Fatalf("NewFileCache(reopen): %v", err)
	}
	got, ok := c2.Get(ctx, "persist")
	if !ok {
		t.Fatalf("expected entry to survive restart")
	}
	if len(got) != 3 || got[0] != 1.25 || got[1] != 2.5 || got[2] != 4.75 {
		t.Fatalf("got: %v", got)
	}
}
