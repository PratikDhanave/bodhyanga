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
)

// BM25 constants. The defaults are the standard Okapi BM25 parameters.
const (
	bm25K1 = 1.5
	bm25B  = 0.75
	rrfK   = 60
)

// hybridStore combines vector cosine similarity with BM25 lexical scoring
// and merges the two rankings using reciprocal rank fusion.
type hybridStore struct {
	embedder embeddings.Embedder

	mu      sync.RWMutex
	docs    []Document
	vectors [][]float32
	index   map[string]int

	// BM25 state, kept in sync with docs/vectors.
	tokens   [][]string       // tokenised content per doc
	tf       []map[string]int // term frequencies per doc
	docLen   []int            // |D| in tokens
	df       map[string]int   // document frequency per term
	totalLen int              // sum of docLen
}

// NewHybridStore returns a Store that blends vector and BM25 retrieval via
// reciprocal-rank fusion (RRF, k=60). It is safe for concurrent Search calls.
func NewHybridStore(embedder embeddings.Embedder) Store {
	if embedder == nil {
		panic("knowledge: NewHybridStore requires a non-nil embedder")
	}
	return &hybridStore{
		embedder: embedder,
		index:    make(map[string]int),
		df:       make(map[string]int),
	}
}

func (s *hybridStore) Index(ctx context.Context, docs []Document) error {
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
		toks := bm25Tokenize(d.Title + " " + d.Content)
		tf := make(map[string]int)
		for _, t := range toks {
			tf[t]++
		}

		if pos, ok := s.index[d.ID]; ok {
			// Roll back the old document's DF contributions before swapping it in.
			s.totalLen -= s.docLen[pos]
			for term := range s.tf[pos] {
				s.df[term]--
				if s.df[term] <= 0 {
					delete(s.df, term)
				}
			}
			s.docs[pos] = d
			s.vectors[pos] = vecs[i]
			s.tokens[pos] = toks
			s.tf[pos] = tf
			s.docLen[pos] = len(toks)
		} else {
			s.index[d.ID] = len(s.docs)
			s.docs = append(s.docs, d)
			s.vectors = append(s.vectors, vecs[i])
			s.tokens = append(s.tokens, toks)
			s.tf = append(s.tf, tf)
			s.docLen = append(s.docLen, len(toks))
		}
		s.totalLen += len(toks)
		for term := range tf {
			s.df[term]++
		}
	}
	return nil
}

func (s *hybridStore) Search(ctx context.Context, query string, k int) ([]Citation, error) {
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

	// 1. Vector ranking.
	vecScores := make([]float64, len(s.docs))
	for i := range s.docs {
		vecScores[i] = cosine(qv, s.vectors[i])
	}
	vecRank := rankOrder(vecScores)

	// 2. BM25 ranking.
	bmScores := s.bm25Scores(query)
	bmRank := rankOrder(bmScores)

	// 3. Reciprocal-rank fusion.
	fused := make([]float64, len(s.docs))
	for i := range s.docs {
		if r := vecRank[i]; r > 0 {
			fused[i] += 1.0 / float64(r+rrfK)
		}
		if r := bmRank[i]; r > 0 {
			fused[i] += 1.0 / float64(r+rrfK)
		}
	}

	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, 0, len(s.docs))
	for i, sc := range fused {
		if sc > 0 {
			scores = append(scores, scored{idx: i, score: sc})
		}
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
	return out, nil
}

func (s *hybridStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.docs)
}

// bm25Scores returns the BM25 score for every indexed document. Caller must
// hold the read lock.
func (s *hybridStore) bm25Scores(query string) []float64 {
	scores := make([]float64, len(s.docs))
	qterms := bm25Tokenize(query)
	if len(qterms) == 0 || len(s.docs) == 0 {
		return scores
	}
	avg := float64(s.totalLen) / float64(len(s.docs))
	if avg == 0 {
		avg = 1
	}
	N := float64(len(s.docs))
	for _, term := range dedupe(qterms) {
		df := float64(s.df[term])
		if df == 0 {
			continue
		}
		idf := math.Log(1 + (N-df+0.5)/(df+0.5))
		for i := range s.docs {
			tf := float64(s.tf[i][term])
			if tf == 0 {
				continue
			}
			dl := float64(s.docLen[i])
			denom := tf + bm25K1*(1-bm25B+bm25B*dl/avg)
			scores[i] += idf * tf * (bm25K1 + 1) / denom
		}
	}
	return scores
}

// rankOrder returns the rank (1-based) of every score; positions whose score
// is <= 0 receive rank 0 (i.e. "not ranked"). Ties are broken by index for
// determinism.
func rankOrder(scores []float64) []int {
	type ix struct {
		idx int
		s   float64
	}
	pairs := make([]ix, 0, len(scores))
	for i, s := range scores {
		if s > 0 {
			pairs = append(pairs, ix{idx: i, s: s})
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].s == pairs[j].s {
			return pairs[i].idx < pairs[j].idx
		}
		return pairs[i].s > pairs[j].s
	})
	ranks := make([]int, len(scores))
	for r, p := range pairs {
		ranks[p.idx] = r + 1
	}
	return ranks
}

// bm25Stopwords is a small English stopword set scoped to BM25 tokenization.
var bm25Stopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "or": {}, "the": {}, "of": {}, "for": {},
	"to": {}, "in": {}, "on": {}, "at": {}, "by": {}, "with": {}, "from": {},
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {},
	"this": {}, "that": {}, "these": {}, "those": {}, "as": {}, "it": {}, "its": {},
	"if": {}, "then": {}, "than": {}, "but": {}, "not": {}, "no": {}, "do": {},
	"does": {}, "did": {}, "has": {}, "have": {}, "had": {}, "will": {}, "would": {},
	"can": {}, "could": {}, "should": {}, "may": {}, "might": {}, "into": {}, "over": {},
}

// bm25Tokenize lowercases, splits on non-alphanumeric runes, and drops stopwords.
func bm25Tokenize(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) == 0 {
			return
		}
		tok := string(cur)
		cur = cur[:0]
		if _, ok := bm25Stopwords[tok]; ok {
			return
		}
		out = append(out, tok)
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			cur = append(cur, r)
		case r >= 'A' && r <= 'Z':
			cur = append(cur, r+('a'-'A'))
		case r >= '0' && r <= '9':
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return out
}

// dedupe returns the unique entries in toks, preserving first-seen order.
func dedupe(toks []string) []string {
	seen := make(map[string]struct{}, len(toks))
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
