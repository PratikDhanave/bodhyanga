// rerank.go — lightweight lexical reranker over an initial top-K.
//
// Overview:
//   Reranker reorders candidate Citations using a corpus-derived
//   rare-term weighting. Construction walks the supplied Documents
//   once to build a document-frequency map; at runtime each candidate
//   is scored by summing log(N / (1 + df(t))) over query tokens that
//   appear in its Title+Snippet. The lexical score is blended with the
//   candidate's existing Score (vector cosine or RRF) at 0.6/0.4 and
//   the top-K survive.
//
//   The reranker is decoupled from the Store interface: any caller can
//   pass in a candidate list from any retrieval path and rerank against
//   any reference corpus. That keeps it composable with both
//   inMemoryStore and hybridStore without coupling.
//
// Key declarations:
//   - Reranker: interface with Rerank(ctx, query, candidates, topK).
//   - NewLexicalReranker(corpus): build df once at construction.
//   - rerankTokenize: lowercase, alphanumeric-only, no stopword filter.
//
// See doc.go for package-level documentation.

package knowledge
