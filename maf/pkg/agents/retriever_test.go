package agents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/embeddings"
	"github.com/PratikDhanave/bodhyanga/pkg/knowledge"
)

const testCorpus = "testdata/corpus.json"

// chestPainWorkup is the canonical case: symptoms that lexically match the
// acs-001 fixture document (BM25 drives ranking under the deterministic mock
// embedder, whose single-element query batch embeds to a zero vector).
func chestPainWorkup() Workup {
	return Workup{
		Query: DiagnosticQuery{
			Case: PatientCase{
				CaseID:   "case-rag-001",
				Symptoms: []string{"acute chest pain", "diaphoresis"},
				History:  []string{"hypertension"},
			},
		},
		Results: []TestResult{{Name: "troponin", Value: "elevated"}},
	}
}

func TestRetrieve_MatchingCaseReturnsCitations(t *testing.T) {
	mock := &embeddings.Mock{}
	r, err := LoadRetriever(testCorpus, mock, 4)
	if err != nil {
		t.Fatalf("LoadRetriever: %v", err)
	}
	cites, err := r.Retrieve(context.Background(), chestPainWorkup())
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(cites) == 0 {
		t.Fatal("want >=1 citation for a matching case, got 0")
	}
	// The ACS guideline must outrank the dermatitis one for a chest-pain case.
	if cites[0].Title == "" || cites[0].Source == "" || cites[0].Snippet == "" {
		t.Errorf("citation fields not populated: %+v", cites[0])
	}
	if cites[0].Source != "demo-synth" {
		t.Errorf("source = %q, want demo-synth", cites[0].Source)
	}
	top := cites[0].Title
	if top != "Acute Coronary Syndrome — Chest Pain Evaluation" {
		t.Errorf("top citation = %q, want the ACS doc (BM25 lexical match)", top)
	}
}

func TestRetrieve_QueryUsesOnlyDeidentifiedFields(t *testing.T) {
	mock := &embeddings.Mock{}
	_, err := LoadRetriever(testCorpus, mock, 4)
	if err != nil {
		t.Fatal(err)
	}
	// buildRetrievalQuery is the seam that must never include the case ID.
	q := buildRetrievalQuery(chestPainWorkup())
	if q == "" {
		t.Fatal("query empty for a populated workup")
	}
	if strings.Contains(q, "case-rag-001") {
		t.Errorf("query leaked the case ID: %q", q)
	}
	for _, want := range []string{"acute chest pain", "diaphoresis", "hypertension", "troponin"} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing de-identified term %q: %q", want, q)
		}
	}
}

func TestRetrieve_NilStoreAndNilReceiverMakeNoEmbedderCall(t *testing.T) {
	// nil receiver.
	var r *Retriever
	cites, err := r.Retrieve(context.Background(), chestPainWorkup())
	if err != nil || cites != nil {
		t.Errorf("nil receiver: got (%v, %v), want (nil, nil)", cites, err)
	}
	// nil store, and prove no embedder was touched.
	mock := &embeddings.Mock{}
	r2 := NewRetriever(nil, 4)
	cites, err = r2.Retrieve(context.Background(), chestPainWorkup())
	if err != nil || cites != nil {
		t.Errorf("nil store: got (%v, %v), want (nil, nil)", cites, err)
	}
	if len(mock.Calls) != 0 {
		t.Errorf("embedder called %d times on nil-store path, want 0", len(mock.Calls))
	}
}

func TestRetrieve_EmptyQueryShortCircuits(t *testing.T) {
	mock := &embeddings.Mock{}
	r, err := LoadRetriever(testCorpus, mock, 4)
	if err != nil {
		t.Fatal(err)
	}
	callsAfterIndex := len(mock.Calls) // index embedded the corpus once
	empty := Workup{Query: DiagnosticQuery{Case: PatientCase{CaseID: "c"}}}
	cites, err := r.Retrieve(context.Background(), empty)
	if err != nil || cites != nil {
		t.Errorf("empty query: got (%v, %v), want (nil, nil)", cites, err)
	}
	if len(mock.Calls) != callsAfterIndex {
		t.Errorf("empty query made a store/embedder call: calls %d -> %d", callsAfterIndex, len(mock.Calls))
	}
}

func TestLoadRetriever_IndexesOnceReusedAcrossCalls(t *testing.T) {
	mock := &embeddings.Mock{}
	r, err := LoadRetriever(testCorpus, mock, 4)
	if err != nil {
		t.Fatal(err)
	}
	indexBatches := len(mock.Calls) // corpus indexed exactly once at load
	if indexBatches == 0 {
		t.Fatal("expected the corpus to be embedded at load")
	}
	if r.store.Size() != 3 {
		t.Fatalf("store size = %d, want 3 fixture docs", r.store.Size())
	}
	for i := 0; i < 3; i++ {
		if _, err := r.Retrieve(context.Background(), chestPainWorkup()); err != nil {
			t.Fatal(err)
		}
	}
	// Size is stable (no re-index) and each Retrieve added exactly one query
	// embed batch on top of the single index batch (FR-011).
	if r.store.Size() != 3 {
		t.Errorf("store re-indexed: size = %d, want 3", r.store.Size())
	}
	if got, want := len(mock.Calls), indexBatches+3; got != want {
		t.Errorf("embedder batches = %d, want %d (index once + 3 queries)", got, want)
	}
}

func TestLoadRetriever_EmptyPathDisablesRAG(t *testing.T) {
	r, err := LoadRetriever("", &embeddings.Mock{}, 4)
	if err != nil {
		t.Fatalf("empty path should not error, got %v", err)
	}
	if r != nil {
		t.Errorf("empty corpus path must disable RAG (nil Retriever), got %+v", r)
	}
}

func TestRetrieve_EmbedderErrorPropagatesWithoutPanic(t *testing.T) {
	mock := &embeddings.Mock{}
	r, err := LoadRetriever(testCorpus, mock, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Flip the shared mock to error AFTER indexing succeeded, simulating a
	// query-time embedder outage.
	mock.Err = errors.New("embedder down")
	cites, err := r.Retrieve(context.Background(), chestPainWorkup())
	if err == nil {
		t.Fatal("want the store error surfaced, got nil")
	}
	if cites != nil {
		t.Errorf("want nil citations on error, got %v", cites)
	}
}

func TestRetrieve_ZeroMatchStoreReturnsNoCitations(t *testing.T) {
	// An empty store (indexed with no documents) yields zero citations, no
	// error — a degrade path distinct from an embedder failure.
	store := knowledge.NewHybridStore(&embeddings.Mock{})
	if err := store.Index(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	r := NewRetriever(store, 4)
	cites, err := r.Retrieve(context.Background(), chestPainWorkup())
	if err != nil {
		t.Fatalf("zero-match store errored: %v", err)
	}
	if len(cites) != 0 {
		t.Errorf("want 0 citations from empty store, got %d", len(cites))
	}
}
