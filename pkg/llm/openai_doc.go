// openai.go — OpenAI chat completions API adapter.
//
// Overview:
//   Implements Client against the OpenAI chat completions API using net/http.
//   System prompts are sent as a system role message. Structured outputs use
//   response_format: json_schema with strict=true; the returned content is
//   validated as JSON before being returned to the caller.
//
// Key declarations:
//   - OpenAIEndpoint: default URL
//   - NewOpenAI(apiKey, model) Client: constructor returning an openai-backed Client
//   - openaiClient: unexported implementation
//   - openaiClient.Stream: SSE streaming implementation of Streamer
//
// See doc.go for package-level documentation.

package llm
