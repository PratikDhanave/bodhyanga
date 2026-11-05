package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/embeddings"
)

// pinnedEmbedder returns canned vectors in the order Embed is called: each
// invocation pops the next vector batch from script. It lets tests control
// exactly which similarity ordering the store will see.
type pinnedEmbedder struct {
	dim    int
	script [][][]float32
	calls  int
}

func (p *pinnedEmbedder) Provider() string { return "pinned" }
func (p *pinnedEmbedder) Model() string    { return "pinned-embed" }
func (p *pinnedEmbedder) Dimension() int   { return p.dim }
func (p *pinnedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if p.calls >= len(p.script) {
		// Default to zero vectors so the test fails loudly rather than panicking.
		out := make([][]float32, len(texts))
		for i := range out {
			out[i] = make([]float32, p.dim)
		}
		p.calls++
		return out, nil
	}
	vecs := p.script[p.calls]
	p.calls++
	if len(vecs) != len(texts) {
		// Pad / truncate to match texts.
		fitted := make([][]float32, len(texts))
		for i := range fitted {
			if i < len(vecs) {
				fitted[i] = vecs[i]
			} else {
				fitted[i] = make([]float32, p.dim)
			}
		}
		return fitted, nil
	}
	return vecs, nil
}

func TestInMemoryIndexAndSearch(t *testing.T) {
	// Three docs, query vector aligned with doc B; doc A is second-closest.
	emb := &pinnedEmbedder{
		dim: 3,
		script: [][][]float32{
			// Index batch: A, B, C
			{
				{0.8, 0.6, 0.0}, // A — moderately aligned with query
				{1.0, 0.0, 0.0}, // B — exactly aligned with query
				{0.0, 0.0, 1.0}, // C — orthogonal
			},
			// Query batch
			{{1.0, 0.0, 0.0}},
		},
	}
	store := NewInMemoryStore(emb)
	ctx := context.Background()
	if err := store.Index(ctx, []Document{
		{ID: "A", Title: "Alpha", Content: "alpha body"},
		{ID: "B", Title: "Bravo", Content: "bravo body"},
		{ID: "C", Title: "Charlie", Content: "charlie body"},
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	got, err := store.Search(ctx, "find bravo", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d", len(got))
	}
	if got[0].DocumentID != "B" || got[1].DocumentID != "A" {
		t.Fatalf("want [B,A], got [%s,%s]", got[0].DocumentID, got[1].DocumentID)
	}
	if got[0].Score < got[1].Score {
		t.Fatalf("scores not descending: %v %v", got[0].Score, got[1].Score)
	}
	if got[0].Score < 0 || got[0].Score > 1 {
		t.Fatalf("score out of [0,1]: %v", got[0].Score)
	}
	if got[0].Snippet == "" {
		t.Fatalf("expected non-empty snippet")
	}
}

func TestInMemoryReplacesOnReIndex(t *testing.T) {
	emb := &pinnedEmbedder{
		dim: 2,
		script: [][][]float32{
			{{1, 0}},
			{{0, 1}}, // replacement with a different vector
			{{1, 0}}, // query
		},
	}
	store := NewInMemoryStore(emb)
	ctx := context.Background()
	if err := store.Index(ctx, []Document{{ID: "x", Title: "old", Content: "first"}}); err != nil {
		t.Fatalf("Index 1: %v", err)
	}
	if err := store.Index(ctx, []Document{{ID: "x", Title: "new", Content: "second"}}); err != nil {
		t.Fatalf("Index 2: %v", err)
	}
	if got := store.Size(); got != 1 {
		t.Fatalf("Size after re-index: want 1, got %d", got)
	}
	cits, err := store.Search(ctx, "anything", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(cits) != 1 {
		t.Fatalf("want 1 result, got %d", len(cits))
	}
	if cits[0].Title != "new" {
		t.Fatalf("want title 'new' (replaced), got %q", cits[0].Title)
	}
}

func TestInMemorySizeReportsCount(t *testing.T) {
	emb := &embeddings.Mock{Dim: 4}
	store := NewInMemoryStore(emb)
	if store.Size() != 0 {
		t.Fatalf("empty store size: want 0, got %d", store.Size())
	}
	docs := []Document{
		{ID: "a", Content: "alpha"},
		{ID: "b", Content: "beta"},
		{ID: "c", Content: "gamma"},
	}
	if err := store.Index(context.Background(), docs); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if store.Size() != 3 {
		t.Fatalf("size after Index: want 3, got %d", store.Size())
	}
}

func TestInMemorySearchWithNoIndexReturnsEmpty(t *testing.T) {
	store := NewInMemoryStore(&embeddings.Mock{Dim: 4})
	got, err := store.Search(context.Background(), "anything", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty results, got %d", len(got))
	}
}

func TestHybridFusesVectorAndBM25(t *testing.T) {
	// Build an embedding script where the vector ranking favours doc X, but
	// the query string only lexically matches doc Y. Without RRF, vector-only
	// would put X on top; with BM25 fused in, Y rises above X.
	emb := &pinnedEmbedder{
		dim: 3,
		script: [][][]float32{
			// Index batch: X, Y, Z
			{
				{1.0, 0.0, 0.0}, // X — perfectly aligned with the query vector
				{0.0, 1.0, 0.0}, // Y — orthogonal in vector space
				{0.0, 0.0, 1.0}, // Z — orthogonal in vector space
			},
			// Query batch
			{{0.9, 0.1, 0.0}},
		},
	}
	store := NewHybridStore(emb)
	ctx := context.Background()
	if err := store.Index(ctx, []Document{
		{ID: "X", Title: "Vector hit", Content: "completely unrelated content about ferns"},
		{ID: "Y", Title: "Lexical hit", Content: "pneumonia cough fever pneumonia pneumonia treatment"},
		{ID: "Z", Title: "Other", Content: "diabetes glycaemic control"},
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	got, err := store.Search(ctx, "pneumonia treatment", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("want >=2 results, got %d", len(got))
	}
	// Both X (rank-1 vector) and Y (rank-1 BM25) should appear; Y should
	// outrank Z, and Y should win the fused ranking because it owns the BM25
	// top spot while X only owns the vector top spot — they tie on combined
	// reciprocal-rank weight, but BM25 also gives Y a non-zero contribution
	// from "treatment". Either way, Y must be in the top-2.
	foundY := false
	for _, c := range got {
		if c.DocumentID == "Y" {
			foundY = true
		}
	}
	if !foundY {
		t.Fatalf("expected Y to surface via BM25 fusion, got %+v", got)
	}
	if got[0].Score <= 0 {
		t.Fatalf("expected positive RRF score, got %v", got[0].Score)
	}
	// Without BM25, vector-only would have put X first AND alone — confirm we
	// got both vector and lexical hits in the top-2.
	if len(got) >= 2 {
		ids := map[string]bool{got[0].DocumentID: true, got[1].DocumentID: true}
		if !(ids["X"] && ids["Y"]) {
			t.Fatalf("expected top-2 to contain both X (vector) and Y (lexical), got %+v", got)
		}
	}
}

func TestLoadCorpusFromTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corpus.json")
	payload := map[string]any{
		"_notice": "demo-synth, not a real guideline",
		"documents": []map[string]any{
			{
				"id":      "cap-001",
				"source":  "demo-synth",
				"title":   "CAP outpatient treatment",
				"content": "Adults with mild CAP and no comorbidities…",
				"metadata": map[string]string{
					"year": "2024",
					"tags": "cap,respiratory",
				},
			},
			{
				"id":      "chf-001",
				"source":  "demo-synth",
				"title":   "ADHF workup",
				"content": "Order BNP / NT-proBNP, chest X-ray, ECG…",
			},
		},
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	docs, err := LoadCorpus(path)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 docs, got %d", len(docs))
	}
	if docs[0].ID != "cap-001" || docs[0].Title == "" || docs[0].Content == "" {
		t.Fatalf("doc[0] malformed: %+v", docs[0])
	}
	if docs[0].Metadata["year"] != "2024" {
		t.Fatalf("doc[0] metadata lost: %+v", docs[0].Metadata)
	}
	if docs[1].ID != "chf-001" {
		t.Fatalf("doc[1] id: want chf-001, got %q", docs[1].ID)
	}
}

func TestLoadCorpusBadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadCorpus(path); err == nil {
		t.Fatalf("LoadCorpus expected error on malformed JSON")
	}
}
