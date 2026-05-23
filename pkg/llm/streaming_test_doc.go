// streaming_test.go — tests for the Streamer implementations across adapters.
//
// Overview:
//   Each test stands up an httptest.Server returning a canned SSE / NDJSON
//   stream and asserts that the corresponding adapter reassembles content
//   chunks correctly, emits ToolCall chunks for streamed tool invocations,
//   and produces a final Done chunk carrying token usage and stop reason.
//   The Mock test verifies the in-memory streamer splits Resp.Content on
//   whitespace and surfaces the same Final response.
//
// Key declarations:
//   - TestMockStream_EmitsContentChunksThenDone
//   - TestAnthropicStream_ParsesContentBlockDeltas
//   - TestAnthropicStream_AccumulatesToolUseBlock
//   - TestOpenAIStream_HandlesDONE
//   - TestOpenAIStream_AccumulatesToolCallArgs
//   - TestOllamaStream_NDJSONReassembly
//   - TestCanStream_DetectsMockAndAdapters
//
// See doc.go for package-level documentation.

package llm
