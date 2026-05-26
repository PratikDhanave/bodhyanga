// client_test.go — table-style coverage for the MCP client surface.
//
// What it tests:
//   - Initialize round-trip (Test*_Initialize)
//   - tools/list, resources/list, resources/read (Test*_ListTools, Test*_ListAndReadResource)
//   - tools/call non-streaming and SSE-streaming (Test*_CallTool, Test*_CallToolStreaming)
//   - JSON-RPC error decoding into *mcp.Error (Test*_CallToolError)
//   - Context-cancellation timeout (Test*_Timeout)
//   - Audit hook: emits one event per CallTool, success and failure, with
//     correlation tags but never tool args or PHI strings (Test*_AuditEmitsOnCallTool)
//   - Auth providers: BearerAuth header presence, NoAuth absence (Test*_AuthBearer, _AuthNoAuth)
//   - Server misbehaviour: 5xx status, malformed JSON, id mismatch (Test*_BadServerResponse)
//   - Stub behaviour: BearerAuth empty token error, SPIFFEAuth ErrNotImplemented,
//     sanitiseEndpoint removes secrets, Recorder no-op with nil sink.
//
// Test design notes:
//   - Every server is a stdlib httptest.Server with an inline handler — no
//     fixtures, no testdata. The handlers assert on request fields inline
//     so test failures point at the exact mismatch.
//   - The streaming test exercises three chunks plus a terminator envelope
//     to verify the merge path; we never rely on goroutine timing.

package mcp
