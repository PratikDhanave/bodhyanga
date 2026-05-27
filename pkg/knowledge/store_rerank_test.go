package knowledge

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/knowledge/rerank"
	"github.com/PratikDhanave/bodh/pkg/observability"
)

// reverseReranker is a deterministic fake: it returns Scored entries in the
// *reverse* order of the input. RerankScore counts down from N so a "higher
// score = better" sort would also flip the order.
type reverseReranker struct{}

func (reverseReranker) Rerank(_ context.Context, _ string, in []rerank.Candidate) ([]rerank.Scored, error) {
	n := len(in)
	out := make([]rerank.Scored, n)
	for i, c := range in {
		out[n-1-i] = rerank.Scored{Candidate: c, RerankScore: float64(i + 1)}
	}
	return out, nil
}

// errReranker returns a fixed error on every call. The store MUST log the
// error and return the unranked input order unchanged.
type errReranker struct{ err error }

func (e errReranker) Rerank(_ context.Context, _ string, _ []rerank.Candidate) ([]rerank.Scored, error) {
	return nil, e.err
}

// indexFixture builds a 3-document in-memory store with a fixed cosine
// ordering of A > B > C. Returns the store and the order Search would
// produce without any reranker.
func indexFixture(t *testing.T, r rerank.Reranker, logger observability.Logger) Store {
	t.Helper()
	emb := &pinnedEmbedder{
		dim: 3,
		script: [][][]float32{
			// Index batch: A, B, C aligned in decreasing alignment with [1,0,0].
			{
				{1.0, 0.0, 0.0},
				{0.6, 0.8, 0.0},
				{0.0, 1.0, 0.0},
			},
			// Query
			{{1.0, 0.0, 0.0}},
		},
	}
	var store Store
	if r == nil {
		store = NewInMemoryStore(emb)
	} else {
		store = NewInMemoryStoreWithReranker(emb, r, logger)
	}
	if err := store.Index(context.Background(), []Document{
		{ID: "A", Title: "Alpha", Content: "alpha body content"},
		{ID: "B", Title: "Bravo", Content: "bravo body content"},
		{ID: "C", Title: "Charlie", Content: "charlie body content"},
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	return store
}

// TestStoreReranker_RunsAndReordersResults verifies that when a Reranker is
// attached, Search returns the candidates in the reranker's order (not the
// upstream cosine order).
func TestStoreReranker_RunsAndReordersResults(t *testing.T) {
	// Baseline: with no reranker, cosine order is A, B, C.
	base := indexFixture(t, nil, nil)
	got, err := base.Search(context.Background(), "find alpha", 3)
	if err != nil {
		t.Fatalf("baseline Search: %v", err)
	}
	if len(got) != 3 || got[0].DocumentID != "A" || got[1].DocumentID != "B" || got[2].DocumentID != "C" {
		t.Fatalf("baseline order want [A,B,C], got %+v", ids(got))
	}

	// With reverseReranker, Search must return C, B, A.
	r := indexFixture(t, reverseReranker{}, nil)
	got, err = r.Search(context.Background(), "find alpha", 3)
	if err != nil {
		t.Fatalf("rerank Search: %v", err)
	}
	if len(got) != 3 || got[0].DocumentID != "C" || got[1].DocumentID != "B" || got[2].DocumentID != "A" {
		t.Fatalf("rerank order want [C,B,A], got %+v", ids(got))
	}
	// Scores must reflect the reranker's RerankScore, not the upstream cosine.
	if got[0].Score < got[1].Score || got[1].Score < got[2].Score {
		t.Fatalf("rerank scores not descending: %v", []float64{got[0].Score, got[1].Score, got[2].Score})
	}
}

// TestStoreReranker_FallsBackOnError verifies that when the reranker fails,
// the original BM25+vector order is returned and the error is logged (not
// surfaced to the caller).
func TestStoreReranker_FallsBackOnError(t *testing.T) {
	var buf bytes.Buffer
	logger := observability.NewLoggerTo(&buf)

	want := errors.New("model unloaded")
	r := indexFixture(t, errReranker{err: want}, logger)
	got, err := r.Search(context.Background(), "find alpha", 3)
	if err != nil {
		t.Fatalf("Search returned reranker error (must be swallowed): %v", err)
	}
	if len(got) != 3 || got[0].DocumentID != "A" || got[1].DocumentID != "B" || got[2].DocumentID != "C" {
		t.Fatalf("on rerank error, want original order [A,B,C], got %+v", ids(got))
	}
	// Failure must be logged so an operator can see it.
	if !bytes.Contains(buf.Bytes(), []byte("reranker error")) {
		t.Fatalf("expected rerank error to be logged, log buffer: %q", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("model unloaded")) {
		t.Fatalf("expected error message in log, log buffer: %q", buf.String())
	}
}

// TestStoreReranker_SingleCandidateSkipped verifies the optimisation that
// skips the reranker call when there's <2 candidates (nothing to reorder).
// We assert this by attaching a reranker that would error if called.
func TestStoreReranker_SingleCandidateSkipped(t *testing.T) {
	emb := &pinnedEmbedder{
		dim:    2,
		script: [][][]float32{{{1, 0}}, {{1, 0}}},
	}
	store := NewInMemoryStoreWithReranker(emb, errReranker{err: errors.New("would-fail")}, nil)
	if err := store.Index(context.Background(), []Document{{ID: "only", Title: "t", Content: "c"}}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	got, err := store.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].DocumentID != "only" {
		t.Fatalf("single-candidate path: want [only], got %+v", ids(got))
	}
}

func ids(cits []Citation) []string {
	out := make([]string, len(cits))
	for i, c := range cits {
		out[i] = c.DocumentID
	}
	return out
}
