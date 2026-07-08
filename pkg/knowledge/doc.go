// Package knowledge implements retrieval-augmented generation (RAG) over a
// clinical corpus.
//
// The package is built around three small types:
//
//   - Document — a unit of indexable content with source, title, body, metadata
//   - Citation — a retrieved snippet with provenance and a similarity score
//   - Store    — Index([]Document) and Search(query, k) operations
//
// The shipped Store is an in-memory vector store backed by an embeddings.Embedder
// and a cosine-similarity ranker, with an optional BM25 lexical pass merged via
// reciprocal-rank fusion for hybrid retrieval. Larger deployments swap this for
// pgvector / Qdrant / Weaviate without touching agent code — the agents only
// see the Store interface.
//
// # Why citations are first-class
//
// Clinical AI requires provenance: every claim the diagnostician makes should
// be traceable back to a guideline or evidence source. The Citation type
// carries source, title, snippet, and score so the agent can include it in
// its DiagnosisProposal — see agents.LLMDiagnostician for how it weaves
// retrieved evidence into the prompt and surfaces citations on the way out.
//
// # Corpus format
//
// LoadCorpus reads a JSON file like data/knowledge/corpus.json:
//
//	{
//	  "documents": [
//	    { "id": "cap-001", "source": "https://example/cap-guideline",
//	      "title": "Community-Acquired Pneumonia — Outpatient Treatment",
//	      "content": "Adults with mild CAP and no comorbidities…",
//	      "metadata": { "guideline_year": "2024", "tags": "cap,respiratory" }
//	    }
//	  ]
//	}
//
// Documents are loaded once at startup and indexed against the configured
// Embedder.
package knowledge
