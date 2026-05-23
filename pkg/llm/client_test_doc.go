// client_test.go — tests for the Mock + Anthropic + OpenAI adapters.
//
// Overview:
//   The tests cover the Mock's behaviour, the Anthropic and OpenAI adapters'
//   happy paths with structured outputs (using httptest.Server to stand in
//   for the provider), the HTTP error path, the bad-JSON path, and the
//   empty-messages guard on both real adapters.
//
// Key declarations:
//   - TestMockRecordsCallsAndReturnsResponse
//   - TestMockReturnsErrorWhenSet
//   - TestAnthropicHappyPathWithJSONSchema
//   - TestAnthropicHTTPErrorBubblesUp
//   - TestOpenAIHappyPathWithJSONSchema
//   - TestOpenAIRejectsNonJSONWhenSchemaRequested
//   - TestEmptyMessagesReturnsErr
//
// See doc.go for package-level documentation.

package llm
