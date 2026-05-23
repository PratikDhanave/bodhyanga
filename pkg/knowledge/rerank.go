package knowledge

import (
	"context"
	"math"
	"sort"
	"strings"
)

// Reranker reorders a candidate Citation list given the query and a
// reference corpus to estimate rare-term frequencies. Returns at most
// topK citations.
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []Citation, topK int) []Citation
}

// lexicalReranker is a small, dependency-free rerank pass. It computes a
// rare-term-weighted overlap between the query and each candidate's
// snippet+title, then blends that with the candidate's existing vector
// score via a 60/40 weighting.
//
// Document frequencies (df) are derived from the reference corpus once,
// at construction time; the candidates passed at runtime can come from
// any Store (a vector store, hybrid store, network search, etc.).
type lexicalReranker struct {
	df          map[string]int
	corpusSize  int
	// lexicalWeight is the weight on the lexical IDF-like score in the
	// final blend. Original (vector / RRF) score gets 1 - lexicalWeight.
	lexicalWeight float64
}

// NewLexicalReranker builds a reranker from a Document corpus. The
// corpus is used only to compute document frequencies; the actual
// candidates passed at runtime can come from any Store.
func NewLexicalReranker(corpus []Document) Reranker {
	df := make(map[string]int)
	for _, d := range corpus {
		text := d.Title + " " + d.Content
		for _, tok := range dedupe(rerankTokenize(text)) {
			df[tok]++
		}
	}
	return &lexicalReranker{
		df:            df,
		corpusSize:    len(corpus),
		lexicalWeight: 0.6,
	}
}

// Rerank implements Reranker. It scores each candidate by summing
// log(N / (1 + df(token))) over query tokens present in the candidate's
// Snippet+Title (the title is included so short snippets still get
// signal), blends with Citation.Score, and returns the top-K.
func (r *lexicalReranker) Rerank(_ context.Context, query string, candidates []Citation, topK int) []Citation {
	if len(candidates) == 0 || topK <= 0 {
		return []Citation{}
	}
	qTokens := dedupe(rerankTokenize(query))
	N := float64(r.corpusSize)
	if N < 1 {
		// No corpus stats — fall back to "use original Score only", but
		// still respect topK so the contract holds.
		N = 1
	}

	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, len(candidates))
	for i, c := range candidates {
		doc := strings.ToLower(c.Title + " " + c.Snippet)
		var lex float64
		for _, qt := range qTokens {
			if qt == "" {
				continue
			}
			if !strings.Contains(doc, qt) {
				continue
			}
			df := float64(r.df[qt])
			lex += math.Log(N / (1.0 + df))
		}
		// Blend; the original Score is already on a roughly comparable
		// 0..1 scale for vector cosine and on a small positive RRF scale
		// for hybrid. Either way we just weight by lexicalWeight.
		blended := r.lexicalWeight*lex + (1.0-r.lexicalWeight)*c.Score
		scores[i] = scored{idx: i, score: blended}
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].score == scores[j].score {
			// Deterministic tiebreaker: original input order.
			return scores[i].idx < scores[j].idx
		}
		return scores[i].score > scores[j].score
	})
	if topK > len(scores) {
		topK = len(scores)
	}
	out := make([]Citation, 0, topK)
	for i := 0; i < topK; i++ {
		c := candidates[scores[i].idx]
		c.Score = scores[i].score
		out = append(out, c)
	}
	return out
}

// rerankTokenize lowercases and splits on non-alphanumeric runes. Kept
// separate from bm25Tokenize so it doesn't drop stopwords — short
// queries like "PE" or "MI" already lose enough signal, and the
// rare-term weighting handles common-word dilution naturally.
func rerankTokenize(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) == 0 {
			return
		}
		out = append(out, string(cur))
		cur = cur[:0]
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
