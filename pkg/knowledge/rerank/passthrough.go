package rerank

import "context"

// Passthrough is the zero-cost Reranker: it preserves input order and
// copies the upstream Score into RerankScore. It is the default when
// no real reranker is configured.
//
// Use it explicitly in two places:
//
//  1. As the default field value when wiring an optional Reranker into a
//     struct — callers that don't care about reranking don't pay for it.
//  2. In tests that want to exercise the "reranker is set" code path
//     without depending on an actual cross-encoder model.
//
// Passthrough is safe for concurrent use; it holds no state.
type Passthrough struct{}

// Rerank implements Reranker. It returns a Scored slice in the same order
// as candidates, with RerankScore == Score.
func (Passthrough) Rerank(_ context.Context, _ string, candidates []Candidate) ([]Scored, error) {
	out := make([]Scored, len(candidates))
	for i, c := range candidates {
		out[i] = Scored{Candidate: c, RerankScore: c.Score}
	}
	return out, nil
}
