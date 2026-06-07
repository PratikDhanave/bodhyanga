package knowledge

import (
	"context"
	"errors"
	"testing"
)

// badCountEmbedder returns a different number of vectors than requested,
// exercising the vector-count mismatch error paths.
type badCountEmbedder struct {
	dim   int
	count int // number of vectors to return regardless of input size
}

func (b *badCountEmbedder) Provider() string { return "bad-count" }
func (b *badCountEmbedder) Model() string    { return "bad-count-embed" }
func (b *badCountEmbedder) Dimension() int   { return b.dim }
func (b *badCountEmbedder) Embed(_ context.Context, _ []string) ([][]float32, error) {
	out := make([][]float32, b.count)
	for i := range out {
		out[i] = make([]float32, b.dim)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// InMemoryStore: Index — embedder returns wrong vector count
// ---------------------------------------------------------------------------

func TestInMemoryStore_Index_VectorCountMismatch(t *testing.T) {
	emb := &badCountEmbedder{dim: 4, count: 5} // always returns 5 vectors
	store := NewInMemoryStore(emb)
	err := store.Index(context.Background(), []Document{
		{ID: "d1", Content: "test"},
		{ID: "d2", Content: "test2"},
	})
	if err == nil {
		t.Fatal("expected error for vector count mismatch")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected non-empty error message")
	}
}

// ---------------------------------------------------------------------------
// InMemoryStore: Search — embedder returns wrong vector count for query
// ---------------------------------------------------------------------------

// searchCountEmbedder returns correct count for Index but wrong count for Search.
type searchCountEmbedder struct {
	dim      int
	calls    int
	queryErr bool // if true, return wrong count on the second call (query)
	queryVec int  // number of vectors to return for query call
}

func (s *searchCountEmbedder) Provider() string { return "search-count" }
func (s *searchCountEmbedder) Model() string    { return "search-count-embed" }
func (s *searchCountEmbedder) Dimension() int   { return s.dim }
func (s *searchCountEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	s.calls++
	if s.calls > 1 && s.queryErr {
		// Return wrong count for query
		out := make([][]float32, s.queryVec)
		for i := range out {
			out[i] = make([]float32, s.dim)
		}
		return out, nil
	}
	// Normal: return correct count
	out := make([][]float32, len(texts))
	for i := range out {
		v := make([]float32, s.dim)
		v[0] = float32(i + 1)
		out[i] = v
	}
	return out, nil
}

func TestInMemoryStore_Search_QueryVectorCountMismatch(t *testing.T) {
	emb := &searchCountEmbedder{dim: 4, queryErr: true, queryVec: 3}
	store := NewInMemoryStore(emb)
	ctx := context.Background()
	if err := store.Index(ctx, []Document{{ID: "d1", Content: "test"}}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	_, err := store.Search(ctx, "query", 5)
	if err == nil {
		t.Fatal("expected error for query vector count mismatch")
	}
}

func TestInMemoryStore_Search_QueryEmbedError(t *testing.T) {
	// Use pinnedEmbedder: first call for Index succeeds, second for query fails.
	emb := &failOnSecondCallEmbedder{dim: 4}
	store := NewInMemoryStore(emb)
	ctx := context.Background()
	if err := store.Index(ctx, []Document{{ID: "d1", Content: "test"}}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	_, err := store.Search(ctx, "query", 5)
	if err == nil {
		t.Fatal("expected error for query embed failure")
	}
}

// failOnSecondCallEmbedder succeeds on first Embed, fails on second.
type failOnSecondCallEmbedder struct {
	dim   int
	calls int
}

func (f *failOnSecondCallEmbedder) Provider() string { return "fail-second" }
func (f *failOnSecondCallEmbedder) Model() string    { return "fail-second-embed" }
func (f *failOnSecondCallEmbedder) Dimension() int   { return f.dim }
func (f *failOnSecondCallEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	if f.calls > 1 {
		return nil, errors.New("embed query failed")
	}
	out := make([][]float32, len(texts))
	for i := range out {
		v := make([]float32, f.dim)
		v[0] = float32(i + 1)
		out[i] = v
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// InMemoryStore: Search — k <= 0 and empty query
// ---------------------------------------------------------------------------

func TestInMemoryStore_Search_KZero(t *testing.T) {
	emb := &pinnedEmbedder{dim: 3, script: [][][]float32{{{1, 0, 0}}}}
	store := NewInMemoryStore(emb)
	ctx := context.Background()
	store.Index(ctx, []Document{{ID: "d1", Content: "test"}})
	cits, err := store.Search(ctx, "test", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cits) != 0 {
		t.Errorf("expected 0 citations for k=0, got %d", len(cits))
	}
}

func TestInMemoryStore_Search_EmptyQuery(t *testing.T) {
	emb := &pinnedEmbedder{dim: 3, script: [][][]float32{{{1, 0, 0}}}}
	store := NewInMemoryStore(emb)
	ctx := context.Background()
	store.Index(ctx, []Document{{ID: "d1", Content: "test"}})
	_, err := store.Search(ctx, "   ", 5)
	if err == nil {
		t.Fatal("expected error for whitespace-only query")
	}
}

// ---------------------------------------------------------------------------
// HybridStore: Index — embedder returns wrong vector count
// ---------------------------------------------------------------------------

func TestHybridStore_Index_VectorCountMismatch(t *testing.T) {
	emb := &badCountEmbedder{dim: 4, count: 5}
	store := NewHybridStore(emb)
	err := store.Index(context.Background(), []Document{
		{ID: "d1", Content: "test"},
		{ID: "d2", Content: "test2"},
	})
	if err == nil {
		t.Fatal("expected error for vector count mismatch")
	}
}

// ---------------------------------------------------------------------------
// HybridStore: Search — query embed error and wrong vector count
// ---------------------------------------------------------------------------

func TestHybridStore_Search_QueryEmbedError(t *testing.T) {
	emb := &failOnSecondCallEmbedder{dim: 4}
	store := NewHybridStore(emb)
	ctx := context.Background()
	if err := store.Index(ctx, []Document{{ID: "d1", Content: "test"}}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	_, err := store.Search(ctx, "query", 5)
	if err == nil {
		t.Fatal("expected error for query embed failure")
	}
}

func TestHybridStore_Search_QueryVectorCountMismatch(t *testing.T) {
	emb := &searchCountEmbedder{dim: 4, queryErr: true, queryVec: 3}
	store := NewHybridStore(emb)
	ctx := context.Background()
	if err := store.Index(ctx, []Document{{ID: "d1", Content: "test"}}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	_, err := store.Search(ctx, "query", 5)
	if err == nil {
		t.Fatal("expected error for query vector count mismatch")
	}
}

func TestHybridStore_Search_KZero(t *testing.T) {
	emb := &pinnedEmbedder{dim: 3, script: [][][]float32{{{1, 0, 0}}}}
	store := NewHybridStore(emb)
	ctx := context.Background()
	store.Index(ctx, []Document{{ID: "d1", Content: "test"}})
	cits, err := store.Search(ctx, "test", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cits) != 0 {
		t.Errorf("expected 0 citations for k=0, got %d", len(cits))
	}
}

// ---------------------------------------------------------------------------
// HybridStore: bm25Scores — edge cases
// ---------------------------------------------------------------------------

func TestHybridStore_Bm25Scores_EmptyDocs(t *testing.T) {
	store := NewHybridStore(&pinnedEmbedder{dim: 3}).(*hybridStore)
	scores := store.bm25Scores("test query")
	if len(scores) != 0 {
		t.Errorf("expected 0 scores for empty store, got %d", len(scores))
	}
}

func TestHybridStore_Bm25Scores_NoMatchingTerms(t *testing.T) {
	store := NewHybridStore(&pinnedEmbedder{
		dim:    3,
		script: [][][]float32{{{1, 0, 0}}},
	}).(*hybridStore)
	ctx := context.Background()
	store.Index(ctx, []Document{{ID: "d1", Content: "alpha bravo charlie"}})
	scores := store.bm25Scores("zzzzz xyzxyz")
	allZero := true
	for _, s := range scores {
		if s != 0 {
			allZero = false
		}
	}
	if !allZero {
		t.Error("expected all-zero scores when no terms match")
	}
}

// ---------------------------------------------------------------------------
// rankOrder: ties broken by index
// ---------------------------------------------------------------------------

func TestRankOrder_TiedScores(t *testing.T) {
	scores := []float64{5.0, 5.0, 3.0, 5.0}
	ranks := rankOrder(scores)
	// Three items with score 5.0 should be ranked 1, 2, 4 (by index order),
	// the item with score 3.0 should be rank 4.
	if ranks[0] != 1 {
		t.Errorf("expected rank 1 for idx 0, got %d", ranks[0])
	}
	if ranks[1] != 2 {
		t.Errorf("expected rank 2 for idx 1, got %d", ranks[1])
	}
	if ranks[3] != 3 {
		t.Errorf("expected rank 3 for idx 3, got %d", ranks[3])
	}
	if ranks[2] != 4 {
		t.Errorf("expected rank 4 for idx 2, got %d", ranks[2])
	}
}

func TestRankOrder_Mixed(t *testing.T) {
	scores := []float64{0, 10, -1, 5, 0}
	ranks := rankOrder(scores)
	// Only scores > 0 get ranked: idx 1 (10.0) -> rank 1, idx 3 (5.0) -> rank 2
	if ranks[1] != 1 {
		t.Errorf("expected rank 1 for idx 1 (score 10), got %d", ranks[1])
	}
	if ranks[3] != 2 {
		t.Errorf("expected rank 2 for idx 3 (score 5), got %d", ranks[3])
	}
	if ranks[0] != 0 || ranks[2] != 0 || ranks[4] != 0 {
		t.Error("expected rank 0 for zero/negative scores")
	}
}

// ---------------------------------------------------------------------------
// HybridStore: Search with metadata present
// ---------------------------------------------------------------------------

func TestHybridStore_Search_WithMetadata(t *testing.T) {
	emb := &pinnedEmbedder{
		dim: 3,
		script: [][][]float32{
			{{1, 0, 0}, {0, 1, 0}},
			{{1, 0, 0}}, // query
		},
	}
	store := NewHybridStore(emb)
	ctx := context.Background()
	err := store.Index(ctx, []Document{
		{ID: "d1", Title: "Doc 1", Content: "alpha test", Source: "s1", Metadata: map[string]string{"k": "v"}},
		{ID: "d2", Title: "Doc 2", Content: "bravo test", Source: "s2"},
	})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	cits, err := store.Search(ctx, "alpha test", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(cits) == 0 {
		t.Fatal("expected results")
	}
	// Verify metadata is copied into citation
	found := false
	for _, c := range cits {
		if c.DocumentID == "d1" && c.Metadata != nil && c.Metadata["k"] == "v" {
			found = true
		}
	}
	if !found {
		t.Error("expected d1 with metadata k=v in results")
	}
}

// ---------------------------------------------------------------------------
// HybridStore: Search — k larger than result count
// ---------------------------------------------------------------------------

func TestHybridStore_Search_KLargerThanDocs(t *testing.T) {
	emb := &pinnedEmbedder{
		dim: 3,
		script: [][][]float32{
			{{1, 0, 0}},
			{{1, 0, 0}}, // query
		},
	}
	store := NewHybridStore(emb)
	ctx := context.Background()
	store.Index(ctx, []Document{{ID: "d1", Content: "test content"}})
	cits, err := store.Search(ctx, "test content", 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(cits) != 1 {
		t.Errorf("expected 1 result when k > docs, got %d", len(cits))
	}
}

// ---------------------------------------------------------------------------
// InMemoryStore: Search — k larger than result count
// ---------------------------------------------------------------------------

func TestInMemoryStore_Search_KLargerThanDocs(t *testing.T) {
	emb := &pinnedEmbedder{
		dim: 3,
		script: [][][]float32{
			{{1, 0, 0}},
			{{1, 0, 0}}, // query
		},
	}
	store := NewInMemoryStore(emb)
	ctx := context.Background()
	store.Index(ctx, []Document{{ID: "d1", Content: "test content"}})
	cits, err := store.Search(ctx, "test content", 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(cits) != 1 {
		t.Errorf("expected 1 result when k > docs, got %d", len(cits))
	}
}

// ---------------------------------------------------------------------------
// InMemoryStore: Index — empty ID
// ---------------------------------------------------------------------------

func TestInMemoryStore_Index_EmptyID(t *testing.T) {
	emb := &pinnedEmbedder{dim: 3}
	store := NewInMemoryStore(emb)
	err := store.Index(context.Background(), []Document{{ID: "", Content: "test"}})
	if err == nil {
		t.Fatal("expected error for empty document ID")
	}
}

// ---------------------------------------------------------------------------
// InMemoryStore: Index — embed error
// ---------------------------------------------------------------------------

func TestInMemoryStore_Index_EmbedError(t *testing.T) {
	emb := &pinnedEmbedder{dim: 3}
	emb.script = nil // will cause zero-vector fallback
	store := NewInMemoryStore(&failAlwaysEmbedder{})
	err := store.Index(context.Background(), []Document{{ID: "d1", Content: "test"}})
	if err == nil {
		t.Fatal("expected error from embedder")
	}
}

// failAlwaysEmbedder always returns an error.
type failAlwaysEmbedder struct{}

func (f *failAlwaysEmbedder) Provider() string { return "fail" }
func (f *failAlwaysEmbedder) Model() string    { return "fail" }
func (f *failAlwaysEmbedder) Dimension() int   { return 4 }
func (f *failAlwaysEmbedder) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, errors.New("embed always fails")
}

// ---------------------------------------------------------------------------
// InMemoryStore: Index — empty docs
// ---------------------------------------------------------------------------

func TestInMemoryStore_Index_EmptyDocs(t *testing.T) {
	store := NewInMemoryStore(&pinnedEmbedder{dim: 3})
	err := store.Index(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.Size() != 0 {
		t.Errorf("expected 0 size, got %d", store.Size())
	}
}
