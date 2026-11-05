// types.go — JSON-RPC 2.0 envelopes and MCP wire-format types.
//
// Overview:
//   Defines every shape Bodhyanga's MCP client serialises across the wire:
//   Request / Response / Error envelopes, plus the MCP-specific Tool /
//   Resource / ToolResult / ContentItem / InitializeResult types.
//
// Key declarations:
//   - JSONRPCVersion, ProtocolVersion: the protocol revision constants.
//   - Method* constants: tools/list, tools/call, resources/list, resources/read, initialize.
//   - Request, Response, Error: JSON-RPC 2.0 envelopes.
//   - Tool, Resource, ToolResult, ContentItem, ResourceContent: MCP payloads.
//   - InitializeParams, InitializeResult, ClientInfo, ServerInfo.
//
// Field-selection rule: every type here matches the MCP spec literally so
// that arbitrary MCP servers (Anthropic, community, in-house) round-trip
// cleanly. Audit-friendly subsets are derived in audit.go, not here.

package mcp
