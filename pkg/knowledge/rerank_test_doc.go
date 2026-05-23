// rerank_test.go — unit tests for the lexical reranker.
//
// Overview:
//   Builds a tiny synthetic corpus where one term ("pulmonary") is rare
//   and the rest are common, then confirms that NewLexicalReranker
//   promotes the candidate whose Snippet/Title contains the rare term
//   even when the two candidates have identical original vector scores.
//   Also exercises the topK contract, an empty corpus (no panic; df=0
//   for every query token), and an empty candidate slice.
//
// Key declarations:
//   - TestLexicalReranker_PromotesRareTermMatches
//   - TestLexicalReranker_RespectsTopK
//   - TestLexicalReranker_EmptyCorpusDoesNotPanic
//   - TestLexicalReranker_EmptyCandidatesReturnsEmpty
//
// See doc.go for package-level documentation.

package knowledge
