// voyage.go — Voyage AI embeddings API adapter.
//
// Overview:
//   Implements Embedder against the Voyage AI embeddings API using net/http.
//   Voyage is Anthropic's recommended embedding provider. Request body and
//   response shape mirror OpenAI's ({"model","input"} → {"data":[{"embedding"}]}).
//   Known model dimensions are hard-coded (voyage-3=1024, voyage-3-lite=512,
//   voyage-large-2=1536); unknown models default to 1024 and refresh from the
//   first response.
//
// Key declarations:
//   - VoyageEndpoint: default URL
//   - NewVoyage(apiKey, model) Embedder: constructor returning a voyage-backed Embedder
//   - voyageEmbedder: unexported implementation
//
// See doc.go for package-level documentation.

package embeddings
