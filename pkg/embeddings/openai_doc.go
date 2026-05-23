// openai.go — OpenAI embeddings API adapter.
//
// Overview:
//   Implements Embedder against the OpenAI embeddings API using net/http.
//   The request body is {"model": "<model>", "input": [<texts>]} and the
//   response is decoded from {"data": [{"embedding": [...], "index": int}]}.
//   Known model dimensions are hard-coded (text-embedding-3-small=1536,
//   text-embedding-3-large=3072, text-embedding-ada-002=1536); unknown models
//   learn their dimension from the first response.
//
// Key declarations:
//   - OpenAIEndpoint: default URL
//   - NewOpenAI(apiKey, model) Embedder: constructor returning an openai-backed Embedder
//   - openaiEmbedder: unexported implementation
//
// See doc.go for package-level documentation.

package embeddings
