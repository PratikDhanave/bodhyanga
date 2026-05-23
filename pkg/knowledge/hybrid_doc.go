// hybrid.go — vector + BM25 retrieval fused with reciprocal rank fusion.
//
// Overview:
//   hybridStore mirrors the in-memory store but additionally maintains BM25
//   bookkeeping (per-document term frequency, document frequency, document
//   lengths, and corpus length). Search produces two rankings — cosine and
//   BM25 (k1=1.5, b=0.75) — and fuses them via reciprocal rank fusion with
//   k=60. The fused score becomes Citation.Score; results are returned top-k
//   by fused score. RRF is robust to score-scale differences across the two
//   rankers and is the workhorse fusion strategy in modern hybrid retrieval.
//
// Key declarations:
//   - NewHybridStore(embedder) Store
//   - hybridStore: docs + vectors + tokens + tf + df + docLen + totalLen
//   - bm25Scores(query): per-document BM25 scores using the standard Okapi formula
//   - rankOrder(scores): 1-based ranks for fusion; zeros for non-scoring positions
//   - bm25Tokenize / bm25Stopwords: lowercase + non-alphanumeric split + small stopword set
//   - rrfK = 60: the canonical RRF constant
//
// See doc.go for package-level documentation.

package knowledge
