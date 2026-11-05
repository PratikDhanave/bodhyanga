// Package server implements the server side of the Model Context Protocol (MCP).
// It exposes a tools.Registry as MCP tools over HTTP+SSE (JSON-RPC 2.0).
//
// Quick start:
//
//	registry := tools.NewRegistry(myTool1, myTool2)
//	srv := server.New(registry, server.Config{Addr: ":8097"})
//	log.Fatal(srv.ListenAndServe())
//
// HTTP routes:
//
//	GET  /sse     — Server-Sent Events endpoint; sends an "endpoint" event
//	               pointing clients at POST /message.
//	POST /message — JSON-RPC 2.0 endpoint; handles initialize, tools/list,
//	               and tools/call.
//
// The server is stateless; every POST /message is a self-contained JSON-RPC
// call. SSE is used only to communicate the message endpoint URL to clients
// (as required by the MCP transport spec).
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/PratikDhanave/bodhyanga/pkg/mcp"
	"github.com/PratikDhanave/bodhyanga/pkg/tools"
)

// DefaultAddr is the listen address used when Config.Addr is empty.
const DefaultAddr = ":8097"

// DefaultName / DefaultVersion are the serverInfo values advertised during the
// initialize handshake when the caller does not supply them in Config.
const (
	DefaultName    = "bodhyanga-mcp-server"
	DefaultVersion = "0.1.0"
)

// Config controls the MCP server's network address and self-identification.
type Config struct {
	// Addr is the TCP listen address, e.g. ":8097" or "127.0.0.1:8097".
	// Defaults to DefaultAddr when empty.
	Addr string

	// Name is the server name reported in the initialize handshake.
	// Defaults to DefaultName when empty.
	Name string

	// Version is the server version reported in the initialize handshake.
	// Defaults to DefaultVersion when empty.
	Version string
}

// Server wraps an http.Server and routes MCP traffic to the embedded registry.
// Construct one with New; the zero value is not usable.
type Server struct {
	http     *http.Server
	registry *tools.Registry
	name     string
	version  string
}

// New creates a Server that exposes every tool in registry as an MCP tool.
// cfg controls the listening address and server identity; zero fields fall back
// to the package defaults.
func New(registry *tools.Registry, cfg Config) *Server {
	if cfg.Addr == "" {
		cfg.Addr = DefaultAddr
	}
	if cfg.Name == "" {
		cfg.Name = DefaultName
	}
	if cfg.Version == "" {
		cfg.Version = DefaultVersion
	}

	s := &Server{
		registry: registry,
		name:     cfg.Name,
		version:  cfg.Version,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/message", s.handleMessage)

	s.http = &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}
	return s
}

// ListenAndServe starts the HTTP server and blocks until it exits. It returns
// http.ErrServerClosed when Shutdown is called.
func (s *Server) ListenAndServe() error {
	return s.http.ListenAndServe()
}

// Shutdown gracefully drains active connections using the supplied context.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// Handler returns the http.Handler used by this Server. Use this to embed the
// MCP server inside an existing http.ServeMux or middleware chain:
//
//	parent.Handle("/mcp/", http.StripPrefix("/mcp", srv.Handler()))
func (s *Server) Handler() http.Handler {
	return s.http.Handler
}

// ─── HTTP handlers ────────────────────────────────────────────────────────────

// handleSSE services GET /sse. It immediately sends an "endpoint" event whose
// data is the URL the client should POST JSON-RPC messages to. After that the
// connection is kept open but no further events are sent; MCP clients typically
// close it after reading the endpoint event.
//
// The MCP HTTP+SSE transport spec requires this event so clients can discover
// the message endpoint without hard-coding it.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Build the POST /message URL. Use the same scheme+host the client used.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	messageURL := fmt.Sprintf("%s://%s/message", scheme, host)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	// Write the "endpoint" event.
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", messageURL)

	// Flush so the client sees the event immediately.
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Block until the client disconnects so the SSE stream stays open.
	<-r.Context().Done()
}

// handleMessage services POST /message. It decodes a single JSON-RPC 2.0
// Request, dispatches to the appropriate handler, and writes the Response.
func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024))
	if err != nil {
		writeError(w, nil, errCodeParseError, "failed to read request body")
		return
	}

	var req mcp.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, nil, errCodeParseError, fmt.Sprintf("parse error: %v", err))
		return
	}
	if req.JSONRPC != mcp.JSONRPCVersion {
		writeError(w, req.ID, errCodeInvalidRequest,
			fmt.Sprintf("unsupported jsonrpc version %q; expected %q", req.JSONRPC, mcp.JSONRPCVersion))
		return
	}

	var result json.RawMessage
	var rpcErr *mcp.Error

	switch req.Method {
	case mcp.MethodInitialize:
		result, rpcErr = s.handleInitialize(r.Context(), req.Params)
	case mcp.MethodToolsList:
		result, rpcErr = s.handleToolsList(r.Context(), req.Params)
	case mcp.MethodToolsCall:
		result, rpcErr = s.handleToolsCall(r.Context(), req.Params)
	default:
		rpcErr = &mcp.Error{Code: errCodeMethodNotFound,
			Message: fmt.Sprintf("method not found: %q", req.Method)}
	}

	if rpcErr != nil {
		writeErrorResp(w, req.ID, rpcErr)
		return
	}
	writeResult(w, req.ID, result)
}

// ─── JSON-RPC method handlers ─────────────────────────────────────────────────

// handleInitialize processes the initialize RPC. It returns the negotiated
// protocol version, server capabilities, and serverInfo.
func (s *Server) handleInitialize(_ context.Context, _ json.RawMessage) (json.RawMessage, *mcp.Error) {
	out := mcp.InitializeResult{
		ProtocolVersion: mcp.ProtocolVersion,
		Capabilities: map[string]any{
			"tools": map[string]any{},
		},
		ServerInfo: mcp.ServerInfo{
			Name:    s.name,
			Version: s.version,
		},
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, internalError(fmt.Sprintf("marshal initialize result: %v", err))
	}
	return raw, nil
}

// handleToolsList processes the tools/list RPC. It iterates the registry and
// returns one descriptor per registered tool.
func (s *Server) handleToolsList(_ context.Context, _ json.RawMessage) (json.RawMessage, *mcp.Error) {
	ts := s.registry.List()
	mcpTools := make([]mcp.Tool, 0, len(ts))
	for _, t := range ts {
		var schema map[string]any
		if raw := t.Schema(); len(raw) > 0 {
			if err := json.Unmarshal(raw, &schema); err != nil {
				// Non-fatal: send nil schema rather than aborting the list.
				schema = nil
			}
		}
		mcpTools = append(mcpTools, mcp.Tool{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: schema,
		})
	}
	out := struct {
		Tools []mcp.Tool `json:"tools"`
	}{Tools: mcpTools}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, internalError(fmt.Sprintf("marshal tools list: %v", err))
	}
	return raw, nil
}

// handleToolsCall processes the tools/call RPC. It looks up the named tool in
// the registry, marshals the arguments to JSON, calls Invoke, and wraps the
// result as a ToolResult with a single text content item.
//
// If the tool is not found a JSON-RPC "method not found" error is returned
// (code -32601). If the tool's Invoke returns an error, a ToolResult with
// IsError=true is returned (MCP-level tool error, not a transport error).
func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (json.RawMessage, *mcp.Error) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &mcp.Error{Code: errCodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err)}
	}
	if p.Name == "" {
		return nil, &mcp.Error{Code: errCodeInvalidParams, Message: "tool name is required"}
	}

	t, ok := s.registry.Get(p.Name)
	if !ok {
		return nil, &mcp.Error{Code: errCodeMethodNotFound,
			Message: fmt.Sprintf("tool not found: %q", p.Name)}
	}

	// Re-marshal arguments to JSON so Invoke receives json.RawMessage.
	var argsRaw json.RawMessage
	if len(p.Arguments) > 0 {
		raw, err := json.Marshal(p.Arguments)
		if err != nil {
			return nil, internalError(fmt.Sprintf("marshal tool arguments: %v", err))
		}
		argsRaw = raw
	} else {
		argsRaw = json.RawMessage("{}")
	}

	resultRaw, invokeErr := t.Invoke(ctx, argsRaw)

	var tr mcp.ToolResult
	if invokeErr != nil {
		tr = mcp.ToolResult{
			IsError: true,
			Content: []mcp.ContentItem{
				{Type: "text", Text: invokeErr.Error()},
			},
		}
	} else {
		// The tool returned a JSON document; surface it as a text content item
		// so MCP clients can parse it without special-casing every tool schema.
		tr = mcp.ToolResult{
			Content: []mcp.ContentItem{
				{Type: "text", Text: string(resultRaw)},
			},
		}
	}

	raw, err := json.Marshal(tr)
	if err != nil {
		return nil, internalError(fmt.Sprintf("marshal tool result: %v", err))
	}
	return raw, nil
}

// ─── response helpers ─────────────────────────────────────────────────────────

// writeResult sends a successful JSON-RPC 2.0 response.
func writeResult(w http.ResponseWriter, id json.RawMessage, result json.RawMessage) {
	resp := mcp.Response{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeError sends a JSON-RPC 2.0 error response using the supplied code and message.
func writeError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeErrorResp(w, id, &mcp.Error{Code: code, Message: message})
}

// writeErrorResp sends a JSON-RPC 2.0 error response.
func writeErrorResp(w http.ResponseWriter, id json.RawMessage, e *mcp.Error) {
	resp := mcp.Response{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      id,
		Error:   e,
	}
	writeJSON(w, http.StatusOK, resp) // JSON-RPC errors ride on HTTP 200.
}

// writeJSON marshals v and writes it as application/json.
func writeJSON(w http.ResponseWriter, status int, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32603,"message":"internal marshal error"}}`,
			http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// internalError is a convenience constructor for JSON-RPC internal errors.
func internalError(msg string) *mcp.Error {
	return &mcp.Error{Code: errCodeInternalError, Message: msg}
}

// JSON-RPC 2.0 error codes. Defined here because pkg/mcp (the client package)
// does not export them; having the constants local to the server avoids an
// import cycle while keeping the values in one place.
//
//	-32700 Parse error      — invalid JSON was received
//	-32600 Invalid request  — the JSON sent is not a valid request object
//	-32601 Method not found — the method does not exist / is not available
//	-32602 Invalid params   — invalid method parameter(s)
//	-32603 Internal error   — internal JSON-RPC error
const (
	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternalError  = -32603
)
