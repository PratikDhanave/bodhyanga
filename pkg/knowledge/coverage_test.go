package knowledge

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/embeddings"
	"github.com/PratikDhanave/bodh/pkg/knowledge/rerank"
)

// ---------------------------------------------------------------------------
// HybridStore: NewHybridStore panics, Size, zero-result search
// ---------------------------------------------------------------------------

func TestNewHybridStore_NilEmbedderPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil embedder")
		}
	}()
	NewHybridStore(nil)
}

func TestHybridStore_Size(t *testing.T) {
	store := NewHybridStore(&embeddings.Mock{})
	if store.Size() != 0 {
		t.Errorf("expected 0 initial size, got %d", store.Size())
	}
	ctx := context.Background()
	store.Index(ctx, []Document{{ID: "d1", Content: "test"}})
	if store.Size() != 1 {
		t.Errorf("expected 1, got %d", store.Size())
	}
}

func TestHybridStore_SearchEmptyStore(t *testing.T) {
	store := NewHybridStore(&embeddings.Mock{})
	cits, err := store.Search(context.Background(), "query", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cits) != 0 {
		t.Errorf("expected 0 citations, got %d", len(cits))
	}
}

func TestHybridStore_SearchEmptyQuery(t *testing.T) {
	store := NewHybridStore(&embeddings.Mock{})
	_, err := store.Search(context.Background(), "   ", 5)
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestHybridStore_IndexEmptyDocs(t *testing.T) {
	store := NewHybridStore(&embeddings.Mock{})
	err := store.Index(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHybridStore_IndexDuplicateID(t *testing.T) {
	store := NewHybridStore(&embeddings.Mock{})
	ctx := context.Background()

	err := store.Index(ctx, []Document{{ID: "d1", Content: "first"}})
	if err != nil {
		t.Fatal(err)
	}
	err = store.Index(ctx, []Document{{ID: "d1", Content: "updated"}})
	if err != nil {
		t.Fatal(err)
	}
	if store.Size() != 1 {
		t.Errorf("expected 1, got %d", store.Size())
	}
}

func TestHybridStore_IndexEmbedError(t *testing.T) {
	m := &embeddings.Mock{Err: errors.New("embed failed")}
	store := NewHybridStore(m)
	err := store.Index(context.Background(), []Document{{ID: "d1", Content: "test"}})
	if err == nil {
		t.Error("expected error from embedder")
	}
}

func TestHybridStore_IndexEmptyID(t *testing.T) {
	store := NewHybridStore(&embeddings.Mock{})
	err := store.Index(context.Background(), []Document{{ID: "", Content: "test"}})
	if err == nil {
		t.Error("expected error for empty ID")
	}
}

// ---------------------------------------------------------------------------
// InMemoryStore: NewInMemoryStore panics, NewInMemoryStoreWithReranker panics
// ---------------------------------------------------------------------------

func TestNewInMemoryStore_NilEmbedderPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil embedder")
		}
	}()
	NewInMemoryStore(nil)
}

func TestNewInMemoryStoreWithReranker_NilEmbedderPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil embedder")
		}
	}()
	NewInMemoryStoreWithReranker(nil, nil, nil)
}

// ---------------------------------------------------------------------------
// snippet: edge cases
// ---------------------------------------------------------------------------

func TestSnippet_ShortBody(t *testing.T) {
	got := snippet("hello", 200)
	if got != "hello" {
		t.Errorf("expected hello, got %q", got)
	}
}

func TestSnippet_LongBody(t *testing.T) {
	body := "word "
	for len(body) < 300 {
		body += "word "
	}
	got := snippet(body, 200)
	if len(got) > 210 {
		t.Errorf("snippet too long: %d chars", len(got))
	}
}

func TestSnippet_EmptyBody(t *testing.T) {
	got := snippet("", 200)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// copyMeta: nil/empty
// ---------------------------------------------------------------------------

func TestCopyMeta_Nil(t *testing.T) {
	got := copyMeta(nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestCopyMeta_Empty(t *testing.T) {
	got := copyMeta(map[string]string{})
	if got != nil {
		t.Errorf("expected nil for empty map, got %v", got)
	}
}

func TestCopyMeta_NonEmpty(t *testing.T) {
	m := map[string]string{"k": "v"}
	got := copyMeta(m)
	if got == nil || got["k"] != "v" {
		t.Error("expected copied map with k=v")
	}
	// Mutation check.
	got["k"] = "modified"
	if m["k"] == "modified" {
		t.Error("copy should not affect original")
	}
}

// ---------------------------------------------------------------------------
// cosine: edge cases
// ---------------------------------------------------------------------------

func TestCosine_UnequalLength(t *testing.T) {
	v := cosine([]float32{1, 2}, []float32{1, 2, 3})
	if v != 0 {
		t.Errorf("expected 0 for unequal length, got %f", v)
	}
}

func TestCosine_ZeroVector(t *testing.T) {
	v := cosine([]float32{0, 0}, []float32{1, 2})
	if v != 0 {
		t.Errorf("expected 0 for zero vector, got %f", v)
	}
}

func TestCosine_Empty(t *testing.T) {
	v := cosine(nil, nil)
	if v != 0 {
		t.Errorf("expected 0 for nil, got %f", v)
	}
}

// ---------------------------------------------------------------------------
// BM25 tokenizer: edge cases
// ---------------------------------------------------------------------------

func TestBm25Tokenize_Stopwords(t *testing.T) {
	tokens := bm25Tokenize("the quick and the fox")
	for _, tok := range tokens {
		if tok == "the" || tok == "and" {
			t.Errorf("stopword %q should be filtered", tok)
		}
	}
}

func TestBm25Tokenize_Numbers(t *testing.T) {
	tokens := bm25Tokenize("version 123")
	found := false
	for _, tok := range tokens {
		if tok == "123" {
			found = true
		}
	}
	if !found {
		t.Error("expected 123 to be preserved")
	}
}

func TestBm25Tokenize_Empty(t *testing.T) {
	tokens := bm25Tokenize("")
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(tokens))
	}
}

// ---------------------------------------------------------------------------
// dedupe
// ---------------------------------------------------------------------------

func TestDedupe(t *testing.T) {
	out := dedupe([]string{"a", "b", "a", "c", "b"})
	if len(out) != 3 {
		t.Errorf("expected 3, got %d", len(out))
	}
}

func TestDedupe_Empty(t *testing.T) {
	out := dedupe(nil)
	if len(out) != 0 {
		t.Errorf("expected 0, got %d", len(out))
	}
}

// ---------------------------------------------------------------------------
// bm25Scores with empty query terms
// ---------------------------------------------------------------------------

func TestHybridStore_Bm25Scores_EmptyQuery(t *testing.T) {
	store := NewHybridStore(&embeddings.Mock{}).(*hybridStore)
	store.Index(context.Background(), []Document{{ID: "d1", Content: "hello world"}})
	// Query with only stopwords.
	scores := store.bm25Scores("the and")
	allZero := true
	for _, s := range scores {
		if s != 0 {
			allZero = false
		}
	}
	if !allZero {
		t.Error("expected all-zero scores for stopword-only query")
	}
}

// ---------------------------------------------------------------------------
// applyReranker edge cases
// ---------------------------------------------------------------------------

type mockReranker struct {
	err     error
	results []rerank.Scored
}

func (m *mockReranker) Rerank(_ context.Context, _ string, _ []rerank.Candidate) ([]rerank.Scored, error) {
	return m.results, m.err
}

type mockLogger struct {
	messages []string
}

func (l *mockLogger) Printf(format string, args ...any) {
	l.messages = append(l.messages, format)
}

func TestApplyReranker_NilReranker(t *testing.T) {
	cits := []Citation{{DocumentID: "d1"}, {DocumentID: "d2"}}
	out := applyReranker(context.Background(), nil, nil, "query", cits)
	if len(out) != 2 {
		t.Error("expected passthrough")
	}
}

func TestApplyReranker_SingleCitation(t *testing.T) {
	r := &mockReranker{}
	cits := []Citation{{DocumentID: "d1"}}
	out := applyReranker(context.Background(), r, nil, "query", cits)
	if len(out) != 1 {
		t.Error("expected passthrough for single citation")
	}
}

func TestApplyReranker_Error(t *testing.T) {
	r := &mockReranker{err: errors.New("rerank failed")}
	l := &mockLogger{}
	cits := []Citation{{DocumentID: "d1"}, {DocumentID: "d2"}}
	out := applyReranker(context.Background(), r, l, "query", cits)
	if len(out) != 2 {
		t.Error("expected fallback on error")
	}
	if len(l.messages) == 0 {
		t.Error("expected log message")
	}
}

func TestApplyReranker_CardinalityMismatch(t *testing.T) {
	r := &mockReranker{results: []rerank.Scored{
		{Candidate: rerank.Candidate{ID: "d1"}, RerankScore: 0.9},
	}}
	l := &mockLogger{}
	cits := []Citation{{DocumentID: "d1"}, {DocumentID: "d2"}}
	out := applyReranker(context.Background(), r, l, "query", cits)
	if len(out) != 2 {
		t.Error("expected fallback on cardinality mismatch")
	}
}

func TestApplyReranker_UnknownID(t *testing.T) {
	r := &mockReranker{results: []rerank.Scored{
		{Candidate: rerank.Candidate{ID: "d1"}, RerankScore: 0.9},
		{Candidate: rerank.Candidate{ID: "unknown"}, RerankScore: 0.5},
	}}
	l := &mockLogger{}
	cits := []Citation{{DocumentID: "d1"}, {DocumentID: "d2"}}
	out := applyReranker(context.Background(), r, l, "query", cits)
	if len(out) != 2 {
		t.Error("expected fallback on unknown ID")
	}
}

func TestApplyReranker_Success(t *testing.T) {
	r := &mockReranker{results: []rerank.Scored{
		{Candidate: rerank.Candidate{ID: "d2"}, RerankScore: 0.9},
		{Candidate: rerank.Candidate{ID: "d1"}, RerankScore: 0.5},
	}}
	l := &mockLogger{}
	cits := []Citation{
		{DocumentID: "d1", Title: "T1", Score: 0.3},
		{DocumentID: "d2", Title: "T2", Score: 0.4},
	}
	out := applyReranker(context.Background(), r, l, "query", cits)
	if len(out) != 2 {
		t.Fatalf("expected 2 results, got %d", len(out))
	}
	if out[0].DocumentID != "d2" {
		t.Errorf("expected d2 first, got %q", out[0].DocumentID)
	}
	if out[0].Score != 0.9 {
		t.Errorf("expected score 0.9, got %f", out[0].Score)
	}
}

// ---------------------------------------------------------------------------
// LoadCorpus: edge case with empty doc ID
// ---------------------------------------------------------------------------

func TestLoadCorpus_FileNotFound(t *testing.T) {
	_, err := LoadCorpus("/nonexistent/path.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadCorpus_BadJSON(t *testing.T) {
	tmpFile := t.TempDir() + "/bad.json"
	os.WriteFile(tmpFile, []byte("not json"), 0o644)
	_, err := LoadCorpus(tmpFile)
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

func TestLoadCorpus_EmptyDocID(t *testing.T) {
	tmpFile := t.TempDir() + "/corpus.json"
	os.WriteFile(tmpFile, []byte(`{"documents":[{"id":"","content":"test"}]}`), 0o644)
	_, err := LoadCorpus(tmpFile)
	if err == nil {
		t.Error("expected error for empty doc ID")
	}
}

func TestLoadCorpus_Success(t *testing.T) {
	tmpFile := t.TempDir() + "/corpus.json"
	os.WriteFile(tmpFile, []byte(`{"documents":[{"id":"d1","content":"test"}]}`), 0o644)
	docs, err := LoadCorpus(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Errorf("expected 1 doc, got %d", len(docs))
	}
}

// ---------------------------------------------------------------------------
// rankOrder: zero scores
// ---------------------------------------------------------------------------

func TestRankOrder_AllZero(t *testing.T) {
	ranks := rankOrder([]float64{0, 0, 0})
	for _, r := range ranks {
		if r != 0 {
			t.Errorf("expected 0 rank for zero score, got %d", r)
		}
	}
}
