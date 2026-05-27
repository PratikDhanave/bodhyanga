package knowledge

import (
	"context"
	"time"

	"github.com/PratikDhanave/bodh/pkg/knowledge/rerank"
	"github.com/PratikDhanave/bodh/pkg/observability"
)

// applyReranker runs the optional cross-encoder reranker over `cits` and
// returns the reordered Citations. The original (BM25+vector) order is
// returned unchanged when:
//
//   - r is nil (no reranker configured)
//   - cits has < 2 entries (nothing to reorder)
//   - r.Rerank returns a non-nil error (treat as "reranker unavailable")
//   - the reranker returns a different number of Scored entries than the
//     input — defensive: a mismatched result implies the caller can't
//     reliably re-key, so degrade to the input order
//
// Failures are logged via `logger` (when non-nil) at the rerank-failed level
// and otherwise swallowed. Per AGENTS.md invariant #3, the agent that called
// Search never observes a "rerank failed" — only a different ordering, or
// the deterministic fallback.
//
// PHI note: `query` and `cits[i].Snippet` are patient-derived and MUST NOT
// be written to the audit log. Operators routing this logger through
// phi.LoggerWrapper get redaction for free; the score / elapsed values
// emitted here are PHI-free either way.
func applyReranker(ctx context.Context, r rerank.Reranker, logger observability.Logger, query string, cits []Citation) []Citation {
	if r == nil || len(cits) < 2 {
		return cits
	}
	candidates := make([]rerank.Candidate, len(cits))
	for i, c := range cits {
		// Title prepended so the cross-encoder sees the same structural
		// signal the embedder did. Snippet alone often misses the topic.
		text := c.Snippet
		if c.Title != "" {
			text = c.Title + "\n" + c.Snippet
		}
		candidates[i] = rerank.Candidate{
			ID:    c.DocumentID,
			Text:  text,
			Score: c.Score,
		}
	}
	start := time.Now()
	scored, err := r.Rerank(ctx, query, candidates)
	elapsed := time.Since(start)
	if err != nil {
		if logger != nil {
			// score-free, PHI-free log line: elapsed + error class only.
			logger.Printf("[knowledge/rerank] reranker error after %s: %v — falling back to upstream order", elapsed, err)
		}
		return cits
	}
	if len(scored) != len(cits) {
		if logger != nil {
			logger.Printf("[knowledge/rerank] cardinality mismatch (got %d for %d) — falling back to upstream order", len(scored), len(cits))
		}
		return cits
	}
	// Build a position map so we can re-key the Scored slice back to the
	// original Citation entries by ID. Defensive: if any ID is missing from
	// the original set, fall back rather than silently dropping a hit.
	byID := make(map[string]Citation, len(cits))
	for _, c := range cits {
		byID[c.DocumentID] = c
	}
	out := make([]Citation, 0, len(scored))
	for _, sc := range scored {
		c, ok := byID[sc.ID]
		if !ok {
			if logger != nil {
				logger.Printf("[knowledge/rerank] unknown id from reranker — falling back to upstream order")
			}
			return cits
		}
		c.Score = sc.RerankScore
		out = append(out, c)
	}
	if logger != nil {
		logger.Printf("[knowledge/rerank] reordered %d candidates in %s", len(out), elapsed)
	}
	return out
}
