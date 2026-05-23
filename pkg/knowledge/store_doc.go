// store.go — the retrieval interface and its data types.
//
// Overview:
//   This file defines what every knowledge backend in the package must
//   implement: Index a slice of Documents, Search a query for top-k
//   Citations, and report Size. The interface is intentionally narrow so
//   in-memory, pgvector, or external vector-DB stores can all plug in
//   behind the same surface.
//
// Key declarations:
//   - Document: ID + Source + Title + Content + Metadata
//   - Citation: DocumentID + Source + Title + Snippet + Score + Metadata
//   - Store: Index(ctx, docs), Search(ctx, query, k), Size()
//
// See doc.go for package-level documentation.

package knowledge
