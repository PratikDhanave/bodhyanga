package agents

import (
	"context"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/embeddings"
	"github.com/PratikDhanave/bodh/pkg/knowledge"
)

// defaultK is the number of citations retrieved when none is configured.
const defaultK = 4

// Retriever grounds the LLM diagnostician in a knowledge corpus. It owns a
// knowledge.Store (bodh's hybrid vector+BM25 store, imported unchanged) and
// builds the retrieval query from de-identified workup content only. It is a
// private detail of the LLM node — the rule diagnostician and the graph never
// see it (plan §Where retrieval sits).
//
// Retrieval is strictly additive and fail-open: a nil Retriever, a nil store,
// or an empty query yields zero citations and makes no embedder call. Only a
// live store error propagates, and the caller swallows it (plan FR-004/FR-005).
type Retriever struct {
	store knowledge.Store
	k     int
}

// NewRetriever wraps a store. k <= 0 selects the default top-k.
func NewRetriever(store knowledge.Store, k int) *Retriever {
	if k <= 0 {
		k = defaultK
	}
	return &Retriever{store: store, k: k}
}

// LoadRetriever loads a JSON corpus, indexes it ONCE with emb, and returns a
// ready Retriever. An empty corpusPath means RAG is disabled: it returns a nil
// Retriever and no error, so callers can inject the result unconditionally
// (plan FR-006, US2). Indexing happens here, never per case (FR-011).
func LoadRetriever(corpusPath string, emb embeddings.Embedder, k int) (*Retriever, error) {
	if strings.TrimSpace(corpusPath) == "" {
		return nil, nil
	}
	docs, err := knowledge.LoadCorpus(corpusPath)
	if err != nil {
		return nil, err
	}
	store := knowledge.NewHybridStore(emb)
	if err := store.Index(context.Background(), docs); err != nil {
		return nil, err
	}
	return NewRetriever(store, k), nil
}

// Retrieve returns up to k citations relevant to the workup. It builds the
// query from the de-identified case symptoms, history, and prior test results
// (a port of bodh's buildRetrievalQuery). A nil receiver, nil store, or empty
// query short-circuits to (nil, nil) with no embedder call. A store error is
// returned for the caller to log-and-swallow — it never panics.
func (r *Retriever) Retrieve(ctx context.Context, wu Workup) ([]Citation, error) {
	if r == nil || r.store == nil {
		return nil, nil
	}
	query := buildRetrievalQuery(wu)
	if query == "" {
		return nil, nil
	}
	cites, err := r.store.Search(ctx, query, r.k)
	if err != nil {
		return nil, err
	}
	out := make([]Citation, 0, len(cites))
	for _, c := range cites {
		out = append(out, Citation{
			Source:  c.Source,
			Title:   c.Title,
			Snippet: c.Snippet,
			Score:   c.Score,
		})
	}
	return out, nil
}

// buildRetrievalQuery composes one query string from de-identified workup
// fields only. PHI is stripped upstream at intake; the case ID is never
// included. Mirrors bodh's agents/diagnostician_llm.go query shape.
func buildRetrievalQuery(wu Workup) string {
	parts := make([]string, 0, len(wu.Query.Case.Symptoms)+len(wu.Query.Case.History)+len(wu.Results))
	parts = append(parts, wu.Query.Case.Symptoms...)
	parts = append(parts, wu.Query.Case.History...)
	for _, t := range wu.Results {
		parts = append(parts, strings.TrimSpace(t.Name+" "+t.Value))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}
