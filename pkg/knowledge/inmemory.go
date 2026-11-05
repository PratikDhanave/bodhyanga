package knowledge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/PratikDhanave/bodhyanga/pkg/embeddings"
	"github.com/PratikDhanave/bodhyanga/pkg/knowledge/rerank"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
)

// inMemoryStore is a thread-safe vector store keyed by document ID. It holds
// every document and its embedding in process memory; Search ranks by cosine
// similarity against a freshly embedded query.
//
// Optional Reranker is the cross-encoder post-stage. When non-nil, Search
// hands the top-k cosine hits to Reranker.Rerank and uses the returned
// order. Errors are logged + ignored (cosine order is preserved) so the
// agent never observes a "rerank failed" — see AGENTS.md invariant #3.
type inMemoryStore struct {
	embedder embeddings.Embedder

	// Reranker is the optional post-stage. nil = no reranker = pass-through.
	Reranker rerank.Reranker
	// Logger receives rerank failures + timings. nil = silent.
	Logger observability.Logger

	mu      sync.RWMutex
	docs    []Document
	vectors [][]float32
	index   map[string]int // doc.ID -> position in docs/vectors
}

// NewInMemoryStore returns a Store backed by an in-process slice of
// embeddings. It is safe for concurrent Search calls.
func NewInMemoryStore(embedder embeddings.Embedder) Store {
	if embedder == nil {
		panic("knowledge: NewInMemoryStore requires a non-nil embedder")
	}
	return &inMemoryStore{
		embedder: embedder,
		index:    make(map[string]int),
	}
}

// NewInMemoryStoreWithReranker is NewInMemoryStore with a configured rerank
// post-stage. Pass logger=nil to silence rerank failure logs; the failure
// itself is always non-fatal (Search returns the cosine order on error).
func NewInMemoryStoreWithReranker(embedder embeddings.Embedder, r rerank.Reranker, logger observability.Logger) Store {
	if embedder == nil {
		panic("knowledge: NewInMemoryStoreWithReranker requires a non-nil embedder")
	}
	return &inMemoryStore{
		embedder: embedder,
		Reranker: r,
		Logger:   logger,
		index:    make(map[string]int),
	}
}

// Index implements Store. All documents are embedded in a single batched
// Embed call. Re-indexing an existing ID replaces its document and vector.
func (s *inMemoryStore) Index(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}
	texts := make([]string, len(docs))
	for i, d := range docs {
		if d.ID == "" {
			return fmt.Errorf("knowledge: document at position %d has empty ID", i)
		}
		texts[i] = embeddingText(d)
	}
	vecs, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("knowledge: embed failed: %w", err)
	}
	if len(vecs) != len(docs) {
		return fmt.Errorf("knowledge: embedder returned %d vectors for %d docs", len(vecs), len(docs))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range docs {
		if pos, ok := s.index[d.ID]; ok {
			s.docs[pos] = d
			s.vectors[pos] = vecs[i]
			continue
		}
		s.index[d.ID] = len(s.docs)
		s.docs = append(s.docs, d)
		s.vectors = append(s.vectors, vecs[i])
	}
	return nil
}

// Search implements Store. Returns up to k Citations ranked by cosine
// similarity against the embedded query. Cosine is in [-1,1] and is
// renormalised to [0,1] for the Citation.Score.
func (s *inMemoryStore) Search(ctx context.Context, query string, k int) ([]Citation, error) {
	if k <= 0 {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("knowledge: empty query")
	}
	s.mu.RLock()
	n := len(s.docs)
	s.mu.RUnlock()
	if n == 0 {
		return nil, nil
	}

	vecs, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("knowledge: embed query failed: %w", err)
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("knowledge: embedder returned %d vectors for query", len(vecs))
	}
	qv := vecs[0]

	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, len(s.docs))
	for i := range s.docs {
		c := cosine(qv, s.vectors[i])
		scores[i] = scored{idx: i, score: (c + 1) / 2}
	}
	sort.SliceStable(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})
	if k > len(scores) {
		k = len(scores)
	}
	out := make([]Citation, 0, k)
	for i := 0; i < k; i++ {
		d := s.docs[scores[i].idx]
		out = append(out, Citation{
			DocumentID: d.ID,
			Source:     d.Source,
			Title:      d.Title,
			Snippet:    snippet(d.Content, 200),
			Score:      scores[i].score,
			Metadata:   copyMeta(d.Metadata),
		})
	}
	return applyReranker(ctx, s.Reranker, s.Logger, query, out), nil
}

// Size implements Store.
func (s *inMemoryStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.docs)
}

// embeddingText is what we feed the embedder. Title carries strong signal,
// so we prepend it.
func embeddingText(d Document) string {
	if d.Title == "" {
		return d.Content
	}
	return d.Title + "\n" + d.Content
}

// cosine returns the cosine similarity between a and b. Vectors of unequal
// length or with zero norm yield 0 rather than NaN/panic.
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		ax, bx := float64(a[i]), float64(b[i])
		dot += ax * bx
		na += ax * ax
		nb += bx * bx
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// snippet returns the first max-ish characters of body, breaking on the
// last word boundary at or before max.
func snippet(body string, max int) string {
	body = strings.TrimSpace(body)
	if len(body) <= max {
		return body
	}
	cut := body[:max]
	if i := strings.LastIndexAny(cut, " \t\n"); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

// copyMeta returns a shallow copy of m so callers can't mutate the stored map.
func copyMeta(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
