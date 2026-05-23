// inmemory.go — thread-safe in-process vector store.
//
// Overview:
//   inMemoryStore holds every Document and its embedding in a slice keyed by
//   ID. Index batches one Embed call across all incoming documents and
//   re-indexes by replacing existing entries. Search embeds the query, scores
//   every stored vector by cosine similarity, renormalises into [0,1], and
//   returns the top-k as Citations with word-boundary snippets. A RWMutex
//   protects the slice so concurrent Search is safe.
//
// Key declarations:
//   - NewInMemoryStore(embedder) Store
//   - inMemoryStore: docs/vectors/index + sync.RWMutex
//   - cosine(a, b): zero-safe cosine similarity helper
//   - snippet(body, max): word-boundary truncation for Citation.Snippet
//   - embeddingText(d): "Title\nContent" so the title is weighted into the vector
//
// See doc.go for package-level documentation.

package knowledge
