// mock.go — in-memory Client for tests.
//
// Overview:
//   A Mock client lets tests inject canned responses (Resp) or errors (Err) and
//   inspect every Request received via Calls. Tests configure the mock and pass
//   it wherever a real client would go.
//
// Key declarations:
//   - Mock: struct with Resp, Err, ProviderName, ModelName, Calls fields
//   - Mock.Provider, Mock.Model, Mock.Complete: implementations of Client
//   - Mock.Stream: implementation of Streamer that splits Resp.Content into words
//
// See doc.go for package-level documentation.

package llm
