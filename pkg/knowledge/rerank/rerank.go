package rerank

import "context"

// Candidate is one entry handed to the reranker. Score is the upstream
// BM25 / vector / RRF score the candidate carried out of the retriever —
// the reranker may use it as a prior, ignore it entirely, or pass it
// through unchanged.
//
// ID and Text are the only fields the reranker model actually reads:
//
//   - ID lets callers re-key the rerank result back to a richer object
//     (knowledge.Citation, a database row, an LLM tool result).
//   - Text is the (query, candidate) pairing input — typically a snippet
//     prefixed with the title so the cross-encoder sees structure.
type Candidate struct {
	ID    string
	Text  string
	Score float64
}

// Scored is one entry returned by Rerank. RerankScore is the cross-encoder
// score; the embedded Candidate carries the original ID + upstream Score so
// callers can rebuild the higher-level result without a second lookup.
//
// Scored slices are returned in descending RerankScore order. Ties keep the
// input order for determinism.
type Scored struct {
	Candidate
	RerankScore float64
}

// Reranker is the post-stage hook fired after BM25 + vector retrieval.
// Implementations are typically a cross-encoder model that reads
// (query, candidate.Text) jointly — orders of magnitude more expensive
// than a vector cosine, which is why this runs on the top-K shortlist
// rather than the full corpus.
//
// Rerank MUST be safe for concurrent use: the knowledge store may call
// it from multiple agents in parallel.
//
// Failure contract: an error return is intentionally non-fatal upstream.
// The knowledge store treats any error as "reranker unavailable" and
// returns the input order unchanged — i.e. the deterministic rule-based
// retrieval result is the fallback. See AGENTS.md invariant #3.
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []Candidate) ([]Scored, error)
}
