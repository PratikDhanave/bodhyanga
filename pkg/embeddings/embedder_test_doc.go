// embedder_test.go — tests for the Mock + OpenAI + Voyage + Ollama adapters.
//
// Overview:
//   The tests cover the Mock's recording, error, and deterministic-vector
//   behaviour; the OpenAI, Voyage, and Ollama adapters' happy paths against
//   an httptest.Server (asserting path/auth/payload and decoding the
//   provider-specific response shape — note Ollama's plural `embeddings`);
//   the OpenAI HTTP error path; NewOllama's default-host fallback and
//   trailing-slash trim; and ErrEmptyInput on all three real adapters.
//
// Key declarations:
//   - TestMockRecordsCallsReturnsResp
//   - TestMockReturnsErr
//   - TestMockGeneratesDeterministicEmbeddings
//   - TestOpenAIHappyPath
//   - TestOpenAIHTTPError
//   - TestVoyageHappyPath
//   - TestOllamaHappyPath
//   - TestNewOllamaFillsDefaultHost
//   - TestEmptyInputReturnsErr
//
// See doc.go for package-level documentation.

package embeddings
