// mock.go — in-memory Embedder for tests.
//
// Overview:
//   A Mock embedder lets tests inject canned vectors (Resp) or errors (Err)
//   and inspect every batch received via Calls. When neither Resp nor Err is
//   set, Mock produces deterministic pseudo-embeddings — the i-th input maps
//   to [float32(i), 0, ..., 0] of length Dimension(). That is enough for
//   simple cosine-similarity tests to distinguish inputs without depending on
//   a real provider.
//
// Key declarations:
//   - Mock: struct with ProviderName, ModelName, Dim, Resp, Err, Calls fields
//   - Mock.Provider, Mock.Model, Mock.Dimension, Mock.Embed: implementations of Embedder
//
// See doc.go for package-level documentation.

package embeddings
