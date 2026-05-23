package knowledge

import "context"

// Document is one indexable unit in the knowledge corpus.
type Document struct {
	// ID uniquely identifies the document within the corpus.
	ID string `json:"id"`

	// Source is a URL, filepath, or human-readable citation pointer
	// (e.g. "USPSTF 2024 §3.2").
	Source string `json:"source"`

	// Title is a short, displayable name.
	Title string `json:"title"`

	// Content is the indexable body. Long documents should be chunked by
	// the caller before indexing.
	Content string `json:"content"`

	// Metadata carries arbitrary string tags (year, condition, evidence
	// level). Retained on Citation results.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Citation is a retrieved document snippet with provenance and a score.
type Citation struct {
	DocumentID string            `json:"document_id"`
	Source     string            `json:"source"`
	Title      string            `json:"title"`
	Snippet    string            `json:"snippet"`
	Score      float64           `json:"score"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Store is the retrieval interface used by RAG-aware agents.
type Store interface {
	// Index ingests documents. Implementations compute and store embeddings.
	// Calling Index again with the same IDs replaces existing entries.
	Index(ctx context.Context, docs []Document) error

	// Search returns up to k Citations ordered by relevance to query.
	Search(ctx context.Context, query string, k int) ([]Citation, error)

	// Size returns the number of indexed documents (for tests and bench).
	Size() int
}
