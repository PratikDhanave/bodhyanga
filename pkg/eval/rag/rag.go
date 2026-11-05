// Package rag implements retrieval-quality and generation-quality metrics for
// RAG (Retrieval-Augmented Generation) pipelines.
//
// Bodhyanga uses pkg/knowledge/ for hybrid BM25+vector retrieval. This package
// measures how well the retrieval stage surfaces relevant documents and how
// faithfully the generation stage uses those documents to answer queries.
//
// Retrieval metrics (reference-based, require ground-truth relevance labels):
//
//   - RecallAtK: fraction of known-relevant documents that appear in the top-k
//   - PrecisionAtK: fraction of the top-k that are actually relevant
//   - MeanReciprocalRank: 1/rank of the first relevant document
//   - NDCG: normalized discounted cumulative gain
//
// Generation metrics (reference-free, work without ground-truth answers):
//
//   - Faithfulness: fraction of answer claims supported by retrieved documents
//   - Relevance: keyword overlap between query and answer (normalized)
//   - Coverage: fraction of key facts from relevant docs that appear in the answer
//
// Typical usage:
//
//	metrics := rag.EvaluateRAG(query, answer, retrieved, relevant, 5)
//	fmt.Printf("Recall@5=%.2f  NDCG@5=%.2f  Faithfulness=%.2f\n",
//	    metrics.RecallAtK, metrics.NDCG, metrics.Faithfulness)
package rag

import (
	"math"
	"strings"
	"unicode"
)

// Document is a retrieved or ground-truth document for RAG evaluation.
type Document struct {
	ID      string  `json:"id"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// RetrievalResult bundles a query with its retrieved documents and
// ground-truth relevant documents for batch evaluation.
type RetrievalResult struct {
	Query     string     `json:"query"`
	Retrieved []Document `json:"retrieved"`
	Relevant  []Document `json:"relevant"`
}

// RAGMetrics captures both retrieval-quality and generation-quality scores
// for a single RAG interaction.
type RAGMetrics struct {
	// Retrieval metrics (reference-based).
	RecallAtK    float64 `json:"recall_at_k"`
	PrecisionAtK float64 `json:"precision_at_k"`
	MRR          float64 `json:"mrr"`
	NDCG         float64 `json:"ndcg"`

	// Generation metrics (reference-free).
	Faithfulness float64 `json:"faithfulness"`
	Relevance    float64 `json:"relevance"`
	Coverage     float64 `json:"coverage"`
}

// RecallAtK returns the fraction of ground-truth relevant documents that
// appear in the top-k of the retrieved list. If k exceeds the length of
// retrieved, all retrieved documents are considered. Returns 0 when
// relevant is empty.
func RecallAtK(retrieved, relevant []Document, k int) float64 {
	if len(relevant) == 0 || k <= 0 {
		return 0
	}
	relSet := relevantIDSet(relevant)
	top := topK(retrieved, k)
	hits := 0
	for _, d := range top {
		if relSet[d.ID] {
			hits++
		}
	}
	return float64(hits) / float64(len(relSet))
}

// PrecisionAtK returns the fraction of the top-k retrieved documents that
// are actually relevant. Returns 0 when k <= 0 or retrieved is empty.
func PrecisionAtK(retrieved, relevant []Document, k int) float64 {
	if k <= 0 || len(retrieved) == 0 {
		return 0
	}
	relSet := relevantIDSet(relevant)
	top := topK(retrieved, k)
	hits := 0
	for _, d := range top {
		if relSet[d.ID] {
			hits++
		}
	}
	return float64(hits) / float64(len(top))
}

// MeanReciprocalRank returns the reciprocal of the rank (1-based) of the
// first relevant document in retrieved. Returns 0 if no relevant document
// is found.
func MeanReciprocalRank(retrieved, relevant []Document) float64 {
	if len(relevant) == 0 || len(retrieved) == 0 {
		return 0
	}
	relSet := relevantIDSet(relevant)
	for i, d := range retrieved {
		if relSet[d.ID] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// NDCG computes the normalized discounted cumulative gain at position k.
// Relevance is binary (1 if the document ID is in relevant, 0 otherwise).
// Returns 0 when relevant is empty or k <= 0.
func NDCG(retrieved, relevant []Document, k int) float64 {
	if len(relevant) == 0 || k <= 0 {
		return 0
	}
	relSet := relevantIDSet(relevant)
	top := topK(retrieved, k)

	// DCG of the actual ranking.
	dcg := 0.0
	for i, d := range top {
		if relSet[d.ID] {
			dcg += 1.0 / math.Log2(float64(i+2)) // i+2 because rank is 1-based, log2(1)=0
		}
	}

	// Ideal DCG: all relevant documents at the top positions.
	idealK := k
	if idealK > len(relSet) {
		idealK = len(relSet)
	}
	idcg := 0.0
	for i := 0; i < idealK; i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// Faithfulness checks whether every claim (sentence) in the answer is
// supported by at least one retrieved document. It splits the answer into
// sentences, then for each sentence looks for keyword overlap with any
// retrieved document's content. Returns the fraction of supported sentences.
//
// This is a lightweight, embedding-free approximation. Production systems
// should use an LLM-as-judge for more accurate faithfulness assessment.
func Faithfulness(answer string, retrievedDocs []Document) float64 {
	sentences := splitSentences(answer)
	if len(sentences) == 0 {
		return 0
	}
	supported := 0
	for _, sent := range sentences {
		if sentenceSupported(sent, retrievedDocs) {
			supported++
		}
	}
	return float64(supported) / float64(len(sentences))
}

// Relevance computes a keyword-overlap score between query and answer,
// normalized to [0, 1]. It measures whether the answer addresses the query
// by checking how many query terms appear in the answer.
//
// This is a lightweight approximation of semantic relevance. Production
// systems should use embedding cosine similarity or an LLM judge.
func Relevance(query, answer string) float64 {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return 0
	}
	answerTokens := tokenizeSet(answer)
	hits := 0
	for _, qt := range queryTokens {
		if answerTokens[qt] {
			hits++
		}
	}
	return float64(hits) / float64(len(queryTokens))
}

// Coverage checks what fraction of key terms from the relevant documents
// appear in the answer. It extracts significant terms (non-stopword tokens)
// from each relevant document and counts how many are present in the answer.
//
// Returns the average per-document coverage.
func Coverage(answer string, relevantDocs []Document) float64 {
	if len(relevantDocs) == 0 {
		return 0
	}
	answerTokens := tokenizeSet(answer)
	totalCoverage := 0.0
	for _, doc := range relevantDocs {
		terms := tokenize(doc.Content)
		if len(terms) == 0 {
			continue
		}
		hits := 0
		for _, t := range terms {
			if answerTokens[t] {
				hits++
			}
		}
		totalCoverage += float64(hits) / float64(len(terms))
	}
	return totalCoverage / float64(len(relevantDocs))
}

// EvaluateRAG runs the full RAG evaluation pipeline for a single query,
// computing all retrieval and generation metrics.
func EvaluateRAG(query, answer string, retrieved, relevant []Document, k int) RAGMetrics {
	return RAGMetrics{
		RecallAtK:    RecallAtK(retrieved, relevant, k),
		PrecisionAtK: PrecisionAtK(retrieved, relevant, k),
		MRR:          MeanReciprocalRank(retrieved, relevant),
		NDCG:         NDCG(retrieved, relevant, k),
		Faithfulness: Faithfulness(answer, retrieved),
		Relevance:    Relevance(query, answer),
		Coverage:     Coverage(answer, relevant),
	}
}

// EvaluateRAGBatch evaluates multiple RAG results and returns the mean
// metrics across all examples. results and answers must have the same
// length; panics otherwise.
func EvaluateRAGBatch(results []RetrievalResult, answers []string, k int) RAGMetrics {
	if len(results) != len(answers) {
		panic("rag: results and answers length mismatch")
	}
	if len(results) == 0 {
		return RAGMetrics{}
	}
	var sum RAGMetrics
	for i, r := range results {
		m := EvaluateRAG(r.Query, answers[i], r.Retrieved, r.Relevant, k)
		sum.RecallAtK += m.RecallAtK
		sum.PrecisionAtK += m.PrecisionAtK
		sum.MRR += m.MRR
		sum.NDCG += m.NDCG
		sum.Faithfulness += m.Faithfulness
		sum.Relevance += m.Relevance
		sum.Coverage += m.Coverage
	}
	n := float64(len(results))
	return RAGMetrics{
		RecallAtK:    sum.RecallAtK / n,
		PrecisionAtK: sum.PrecisionAtK / n,
		MRR:          sum.MRR / n,
		NDCG:         sum.NDCG / n,
		Faithfulness: sum.Faithfulness / n,
		Relevance:    sum.Relevance / n,
		Coverage:     sum.Coverage / n,
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// relevantIDSet builds a set of document IDs from the relevant slice for
// O(1) lookups.
func relevantIDSet(relevant []Document) map[string]bool {
	s := make(map[string]bool, len(relevant))
	for _, d := range relevant {
		s[d.ID] = true
	}
	return s
}

// topK returns the first min(k, len(docs)) documents.
func topK(docs []Document, k int) []Document {
	if k > len(docs) {
		k = len(docs)
	}
	return docs[:k]
}

// splitSentences splits text on sentence-ending punctuation (., !, ?).
// Empty sentences are dropped. This is intentionally simple; a production
// implementation would use a proper NLP sentence splitter.
func splitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var sentences []string
	var cur strings.Builder
	for _, r := range text {
		cur.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				sentences = append(sentences, s)
			}
			cur.Reset()
		}
	}
	// Trailing fragment without terminal punctuation.
	if s := strings.TrimSpace(cur.String()); s != "" {
		sentences = append(sentences, s)
	}
	return sentences
}

// sentenceSupported returns true if the sentence shares enough keyword
// overlap with at least one retrieved document. The threshold is set low
// (2 matching significant terms) to avoid false negatives.
func sentenceSupported(sentence string, docs []Document) bool {
	sentTokens := tokenize(sentence)
	if len(sentTokens) == 0 {
		return false
	}
	sentSet := make(map[string]bool, len(sentTokens))
	for _, t := range sentTokens {
		sentSet[t] = true
	}
	for _, doc := range docs {
		docTokens := tokenize(doc.Content)
		hits := 0
		for _, dt := range docTokens {
			if sentSet[dt] {
				hits++
			}
		}
		// Require at least 2 matching terms, or all terms if the sentence
		// has fewer than 2.
		threshold := 2
		if len(sentTokens) < threshold {
			threshold = len(sentTokens)
		}
		if hits >= threshold {
			return true
		}
	}
	return false
}

// stopwords is a small English stopword set for tokenization.
var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "or": true, "the": true,
	"of": true, "for": true, "to": true, "in": true, "on": true,
	"at": true, "by": true, "with": true, "from": true, "is": true,
	"are": true, "was": true, "were": true, "be": true, "been": true,
	"being": true, "this": true, "that": true, "these": true,
	"those": true, "as": true, "it": true, "its": true, "if": true,
	"then": true, "than": true, "but": true, "not": true, "no": true,
	"do": true, "does": true, "did": true, "has": true, "have": true,
	"had": true, "will": true, "would": true, "can": true, "could": true,
	"should": true, "may": true, "might": true, "into": true, "over": true,
	"also": true, "such": true, "which": true, "what": true, "when": true,
	"where": true, "who": true, "how": true,
}

// tokenize lowercases, splits on non-alphanumeric runes, and drops
// stopwords. Returns a slice of significant tokens (may contain duplicates).
func tokenize(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) == 0 {
			return
		}
		tok := string(cur)
		cur = cur[:0]
		if stopwords[tok] {
			return
		}
		out = append(out, tok)
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return out
}

// tokenizeSet tokenizes s and returns the unique tokens as a set.
func tokenizeSet(s string) map[string]bool {
	toks := tokenize(s)
	set := make(map[string]bool, len(toks))
	for _, t := range toks {
		set[t] = true
	}
	return set
}
