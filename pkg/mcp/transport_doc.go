// transport.go — JSON-RPC 2.0 round-trip over HTTP, with optional SSE.
//
// Overview:
//   transport is the wire layer underneath every Client method. Each
//   client method composes its params into the transport's call(method,
//   params) entry point; the transport handles ID minting, body marshaling,
//   AuthProvider invocation, HTTP POST, and response decoding. Both
//   delivery modes are supported on the same code path:
//
//   1. application/json — single Response in the body, the common case for
//      initialize / tools/list / resources/list.
//   2. text/event-stream — SSE-framed sequence of payloads. Each "data:"
//      line is one JSON object; the transport assembles partial ToolResult
//      chunks into a final merged ToolResult so streaming tool calls look
//      identical to the caller as inline responses.
//
// Key declarations:
//   - transport: the HTTP+SSE round-trip implementation, owned by HTTPClient.
//   - transport.call(ctx, method, params): builds + dispatches one JSON-RPC
//     request and returns the decoded *Response.
//   - transport.readJSON / transport.readSSE: the two response-decoding paths.
//   - decodeResult(resp, v): unmarshal result into v, returning *Error for
//     JSON-RPC failures so callers can distinguish protocol vs transport
//     errors with a single errors.As check.
//
// JSON-RPC contract: every Request carries jsonrpc=="2.0", a numeric id, a
// method, and (optionally) params. Responses echo the id; non-streaming
// responses are id-matched against the originating request to guard against
// proxy reordering. SSE responses skip the id check because servers may
// omit ids on partial chunks.
//
// No third-party JSON-RPC library: the wire format is simple and we avoid
// the dependency cost.

package mcp
