// Package mcp is Bodh's client for the Model Context Protocol (MCP) — the
// emerging JSON-RPC 2.0 standard for exposing tools and resources to LLM
// agents. It implements the wire-format client described in Proposal 1 of
// docs/agent-interop.md.
//
// What this package gives you
//
//   - A provider-agnostic Client interface modelled on the MCP spec:
//     Initialize / ListTools / CallTool / ListResources / ReadResource / Close.
//   - An HTTPClient that speaks JSON-RPC 2.0 over HTTP, with optional
//     server-sent-events (SSE) framing for streaming tool results.
//   - Pluggable AuthProvider implementations (BearerAuth, NoAuth, and a
//     SPIFFEAuth stub that returns ErrNotImplemented until Proposal 6 lands).
//   - A Recorder hook that emits a PHI-free audit.Event for every CallTool
//     invocation — server URL, tool name, outcome, duration, tenant/case
//     correlation IDs. Never tool arguments or results.
//
// # Why MCP
//
// Bodh's existing pkg/tools holds internal Go tool helpers. They work for the
// demo, but every external lab / imaging / FHIR integration needs a new Go
// adapter. MCP is the path of least resistance — any vendor-published MCP
// server becomes available with zero adapter code. See Proposal 1 in
// docs/agent-interop.md for the full impact analysis.
//
// What this package does NOT do (deferred to follow-up PRs)
//
//   - Refactoring agents/diagnostician_tools.go to call the new client. This
//     PR ships the client in isolation so the refactor can be reviewed
//     separately.
//   - The MCPPolicyEnforcer wrapper that composes governance.Policy in front
//     of every CallTool. It's a small, focused PR of its own.
//   - The stdio transport for local-process MCP servers. HTTP+SSE is the
//     right primary transport; stdio is a follow-up.
//   - The MCP server façade exposing Bodh's own pkg/tools (Proposal 8).
//   - Real SPIFFE/SPIRE integration (Proposal 6).
//
// # JSON-RPC 2.0 contract
//
// Every request carries jsonrpc="2.0", a string or integer id, a method name,
// and a params object. Every response echoes the id and contains exactly one
// of result or error. The transport in transport.go is responsible for
// matching ids and surfacing JSON-RPC errors as *mcp.Error so callers can
// distinguish protocol failures from transport failures.
//
// # Audit and PHI hygiene
//
// audit.go emits one CallEvent per CallTool. The event records that a call
// happened (server URL, tool name, duration, outcome, tenant/case/trace
// correlation) but never the tool arguments or response content. Those
// frequently carry PHI; surfacing them in the audit log defeats the
// "minimum-necessary" principle the rest of Bodh's audit pipeline follows.
// See pkg/audit/recorder.go for the field-selection rationale.
package mcp
