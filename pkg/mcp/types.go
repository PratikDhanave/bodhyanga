package mcp

import (
	"encoding/json"
	"fmt"
)

// JSONRPCVersion is the protocol version every Request and Response must
// carry. MCP fixes this at "2.0".
const JSONRPCVersion = "2.0"

// ProtocolVersion is the MCP protocol revision Bodh's client targets.
// MCP is versioned by date stamps; the value Initialize sends is configurable
// via Option but defaults to this constant.
const ProtocolVersion = "2024-11-05"

// Methods used by the client. Centralised here so tests can reference them
// without string-typing.
const (
	MethodInitialize    = "initialize"
	MethodToolsList     = "tools/list"
	MethodToolsCall     = "tools/call"
	MethodResourcesList = "resources/list"
	MethodResourcesRead = "resources/read"
)

// Request is a JSON-RPC 2.0 request envelope. ID is an opaque correlation
// token; this client uses monotonically incrementing integers, but the spec
// also allows strings.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response envelope. Exactly one of Result and
// Error is populated on a successful decode.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is the JSON-RPC 2.0 error object. Code follows the JSON-RPC numeric
// convention (-32700 parse error, -32600 invalid request, -32601 method not
// found, -32602 invalid params, -32603 internal error; -32000..-32099 are
// reserved for server-defined errors).
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface so JSON-RPC errors flow naturally
// through Go's error-handling.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("mcp: jsonrpc error %d: %s", e.Code, e.Message)
}

// Tool is one tool exposed by an MCP server. InputSchema is the JSON Schema
// describing the arguments CallTool should send.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// Resource is one read-only data source exposed by an MCP server. URI is the
// opaque identifier the server uses; ReadResource takes it back as input.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ToolResult is what CallTool returns. IsError is the MCP-level "the tool
// ran but reported an error" flag; transport / JSON-RPC errors are surfaced
// as Go errors, not in this field.
type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem is one chunk of returned content. Type is "text", "image", or
// "resource". For "text" the payload is Text; for "image" the payload is
// Data (base64-encoded) plus MimeType; for "resource" the payload is a
// reference encoded into Resource.
type ContentItem struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	Data     string    `json:"data,omitempty"`
	MimeType string    `json:"mimeType,omitempty"`
	Resource *Resource `json:"resource,omitempty"`
}

// ResourceContent is what ReadResource returns. Contents is one or more
// chunks; for text resources each Content holds Text, for binary resources
// Data is base64-encoded.
type ResourceContent struct {
	Contents []ResourceContentItem `json:"contents"`
}

// ResourceContentItem is one chunk inside a ResourceContent.
type ResourceContentItem struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
}

// InitializeParams is the params object for the initialize call.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
}

// ClientInfo identifies the calling client in the initialize handshake.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerInfo identifies the server, returned from initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the server's response to initialize.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
}

// listToolsResult mirrors the tools/list response shape; unexported because
// only ListTools consumes it.
type listToolsResult struct {
	Tools []Tool `json:"tools"`
}

// listResourcesResult mirrors the resources/list response shape.
type listResourcesResult struct {
	Resources []Resource `json:"resources"`
}

// callToolParams is the params shape for tools/call.
type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// readResourceParams is the params shape for resources/read.
type readResourceParams struct {
	URI string `json:"uri"`
}
