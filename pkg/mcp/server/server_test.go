package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/mcp"
	"github.com/PratikDhanave/bodhyanga/pkg/mcp/server"
	"github.com/PratikDhanave/bodhyanga/pkg/tools"
)

// ─── helpers ────────────────────────────────────────────────────────────────

// newTestServer builds a Server backed by a single echo tool and returns the
// underlying http.Handler (so tests can drive it via httptest without listening
// on a real port).
func newTestServer(t *testing.T) (http.Handler, *tools.Registry) {
	t.Helper()
	reg := tools.NewRegistry(tools.DrugInteraction{}, tools.FHIRQuery{}, tools.DefaultRiskCalculator())
	srv := server.New(reg, server.Config{
		Addr:    ":0", // unused — we call Handler() directly
		Name:    "test-server",
		Version: "0.0.1",
	})
	return srv.Handler(), reg
}

// post sends a JSON-RPC 2.0 request to POST /message and returns the Response.
func post(t *testing.T, h http.Handler, method string, params any) *mcp.Response {
	t.Helper()
	var paramsRaw json.RawMessage
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		paramsRaw = raw
	}
	req := mcp.Request{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  method,
		Params:  paramsRaw,
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/message", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected HTTP status %d: %s", w.Code, w.Body.String())
	}

	var resp mcp.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, w.Body.String())
	}
	return &resp
}

// ─── initialize ─────────────────────────────────────────────────────────────

func TestInitialize(t *testing.T) {
	h, _ := newTestServer(t)

	params := map[string]any{
		"protocolVersion": mcp.ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test-client", "version": "1.0"},
	}
	resp := post(t, h, mcp.MethodInitialize, params)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("result is nil")
	}

	var result mcp.InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ProtocolVersion != mcp.ProtocolVersion {
		t.Errorf("protocol version: got %q want %q", result.ProtocolVersion, mcp.ProtocolVersion)
	}
	if result.ServerInfo.Name != "test-server" {
		t.Errorf("server name: got %q want %q", result.ServerInfo.Name, "test-server")
	}
	if result.ServerInfo.Version != "0.0.1" {
		t.Errorf("server version: got %q want %q", result.ServerInfo.Version, "0.0.1")
	}
	if _, ok := result.Capabilities["tools"]; !ok {
		t.Error("capabilities missing 'tools' key")
	}
}

// ─── tools/list ─────────────────────────────────────────────────────────────

func TestToolsList(t *testing.T) {
	h, _ := newTestServer(t)

	resp := post(t, h, mcp.MethodToolsList, nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var out struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// We registered three tools.
	if len(out.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(out.Tools))
	}

	// Tools are returned in sorted (name-ascending) order by the registry.
	names := make([]string, len(out.Tools))
	for i, tool := range out.Tools {
		names[i] = tool.Name
	}
	want := []string{"drug_interaction", "fhir_query", "risk_calculator"}
	for i, name := range want {
		if names[i] != name {
			t.Errorf("tool[%d]: got %q want %q", i, names[i], name)
		}
	}

	// Each tool should have a non-empty description.
	for _, tool := range out.Tools {
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
	}

	// Each tool should have a non-nil InputSchema.
	for _, tool := range out.Tools {
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil InputSchema", tool.Name)
		}
	}
}

// ─── tools/call ─────────────────────────────────────────────────────────────

func TestToolsCallDrugInteraction(t *testing.T) {
	h, _ := newTestServer(t)

	params := map[string]any{
		"name": "drug_interaction",
		"arguments": map[string]any{
			"drugs": []any{"warfarin", "ibuprofen"},
		},
	}
	resp := post(t, h, mcp.MethodToolsCall, params)
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %v", resp.Error)
	}

	var tr mcp.ToolResult
	if err := json.Unmarshal(resp.Result, &tr); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}
	if tr.IsError {
		t.Fatalf("tool reported IsError; content: %v", tr.Content)
	}
	if len(tr.Content) == 0 {
		t.Fatal("empty content")
	}
	if tr.Content[0].Type != "text" {
		t.Errorf("content type: got %q want %q", tr.Content[0].Type, "text")
	}
	// The result should mention the interaction.
	if !strings.Contains(tr.Content[0].Text, "warfarin") {
		t.Errorf("expected warfarin in result text, got: %s", tr.Content[0].Text)
	}
}

func TestToolsCallFHIRQuery(t *testing.T) {
	h, _ := newTestServer(t)

	params := map[string]any{
		"name": "fhir_query",
		"arguments": map[string]any{
			"resource":   "Patient",
			"patient_id": "pt-001",
		},
	}
	resp := post(t, h, mcp.MethodToolsCall, params)
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %v", resp.Error)
	}

	var tr mcp.ToolResult
	if err := json.Unmarshal(resp.Result, &tr); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}
	if tr.IsError {
		t.Fatalf("tool reported IsError; content: %v", tr.Content)
	}
	if len(tr.Content) == 0 {
		t.Fatal("empty content")
	}
	if !strings.Contains(tr.Content[0].Text, "Jane Doe") {
		t.Errorf("expected 'Jane Doe' in result, got: %s", tr.Content[0].Text)
	}
}

func TestToolsCallRiskCalculator(t *testing.T) {
	h, _ := newTestServer(t)

	params := map[string]any{
		"name": "risk_calculator",
		"arguments": map[string]any{
			"calculator": "wells_pe",
			"inputs": map[string]any{
				"clinical_signs_dvt": true,
				"alt_dx_less_likely": true,
				"hr_over_100":        false,
			},
		},
	}
	resp := post(t, h, mcp.MethodToolsCall, params)
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %v", resp.Error)
	}

	var tr mcp.ToolResult
	if err := json.Unmarshal(resp.Result, &tr); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}
	if tr.IsError {
		t.Fatalf("tool reported IsError; content: %v", tr.Content)
	}
	if len(tr.Content) == 0 {
		t.Fatal("empty content")
	}
	// Should include wells_pe score info.
	if !strings.Contains(tr.Content[0].Text, "wells_pe") {
		t.Errorf("expected 'wells_pe' in result, got: %s", tr.Content[0].Text)
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	h, _ := newTestServer(t)

	params := map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]any{},
	}
	resp := post(t, h, mcp.MethodToolsCall, params)
	// Should return a JSON-RPC error for unknown tool.
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected method-not-found code -32601, got %d", resp.Error.Code)
	}
}

func TestToolsCallToolError(t *testing.T) {
	h, _ := newTestServer(t)

	// drug_interaction with empty drugs list should surface as IsError=true.
	params := map[string]any{
		"name": "drug_interaction",
		"arguments": map[string]any{
			"drugs": []any{},
		},
	}
	resp := post(t, h, mcp.MethodToolsCall, params)
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %v", resp.Error)
	}

	var tr mcp.ToolResult
	if err := json.Unmarshal(resp.Result, &tr); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for invalid tool input")
	}
}

// ─── unknown method ─────────────────────────────────────────────────────────

func TestUnknownMethod(t *testing.T) {
	h, _ := newTestServer(t)

	resp := post(t, h, "nosuchmethod/foo", nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method, got nil")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected -32601 method-not-found, got %d", resp.Error.Code)
	}
}

// ─── malformed request ───────────────────────────────────────────────────────

func TestMalformedJSON(t *testing.T) {
	h, _ := newTestServer(t)

	r := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader("{invalid"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	var resp mcp.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected parse error, got nil error")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("expected -32700 parse error, got %d", resp.Error.Code)
	}
}

// ─── GET /sse ────────────────────────────────────────────────────────────────

func TestSSEEndpointEvent(t *testing.T) {
	h, _ := newTestServer(t)

	// Use a cancelable context so the SSE handler exits after we've read the
	// first event — otherwise ServeHTTP blocks forever waiting for client disconnect.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/sse", nil)
	r.Host = "localhost:8097"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	body := w.Body.String()
	if !strings.Contains(body, "event: endpoint") {
		t.Errorf("expected 'event: endpoint' in SSE body, got: %s", body)
	}
	if !strings.Contains(body, "/message") {
		t.Errorf("expected '/message' URL in SSE body, got: %s", body)
	}
}

func TestSSEWrongMethod(t *testing.T) {
	h, _ := newTestServer(t)

	r := httptest.NewRequest(http.MethodPost, "/sse", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestMessageWrongMethod(t *testing.T) {
	h, _ := newTestServer(t)

	r := httptest.NewRequest(http.MethodGet, "/message", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
