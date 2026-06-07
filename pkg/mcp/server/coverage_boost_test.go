package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/mcp"
	"github.com/PratikDhanave/bodh/pkg/tools"
)

// ---------------------------------------------------------------------------
// ListenAndServe + Shutdown
// ---------------------------------------------------------------------------

func TestListenAndServe_Shutdown(t *testing.T) {
	reg := tools.NewRegistry()
	// Use port 0 to get an ephemeral port.
	s := New(reg, Config{Addr: "127.0.0.1:0"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.ListenAndServe()
	}()

	// Give the server a moment to start.
	time.Sleep(100 * time.Millisecond)

	// Shut it down.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// ListenAndServe should return http.ErrServerClosed.
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Fatalf("expected ErrServerClosed, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ListenAndServe did not return after Shutdown")
	}
}

// ---------------------------------------------------------------------------
// handleSSE — TLS path (r.TLS != nil)
// ---------------------------------------------------------------------------

func TestHandleSSE_TLSScheme(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	ctx, cancel := context.WithCancel(context.Background())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	req = req.WithContext(ctx)
	// Simulate TLS by setting the TLS field.
	req.TLS = &tls.ConnectionState{}

	done := make(chan struct{})
	go func() {
		s.handleSSE(rr, req)
		close(done)
	}()

	// Give it a moment to write, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	body := rr.Body.String()
	if !strings.Contains(body, "https://") {
		t.Errorf("expected https:// in SSE endpoint, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// handleSSE — Host from r.URL.Host fallback
// ---------------------------------------------------------------------------

func TestHandleSSE_HostFallback(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	ctx, cancel := context.WithCancel(context.Background())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://fallback-host:8080/sse", nil)
	req.Host = "" // Clear the Host header to trigger fallback.
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		s.handleSSE(rr, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	body := rr.Body.String()
	if !strings.Contains(body, "endpoint") {
		t.Errorf("expected endpoint event, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// handleInitialize — success path with custom name/version
// ---------------------------------------------------------------------------

func TestHandleInitialize_CustomNameVersion(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{Name: "test-server", Version: "2.0"})

	body := `{"jsonrpc":"2.0","method":"initialize","id":1,"params":{}}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	var result mcp.InitializeResult
	json.Unmarshal(resp.Result, &result)
	if result.ServerInfo.Name != "test-server" {
		t.Errorf("name = %q, want test-server", result.ServerInfo.Name)
	}
	if result.ServerInfo.Version != "2.0" {
		t.Errorf("version = %q, want 2.0", result.ServerInfo.Version)
	}
	if result.ProtocolVersion != mcp.ProtocolVersion {
		t.Errorf("protocol = %q, want %q", result.ProtocolVersion, mcp.ProtocolVersion)
	}
}

// ---------------------------------------------------------------------------
// handleToolsList — empty registry
// ---------------------------------------------------------------------------

func TestHandleToolsList_EmptyRegistry(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	body := `{"jsonrpc":"2.0","method":"tools/list","id":1}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// handleToolsCall — tool with schema
// ---------------------------------------------------------------------------

func TestHandleToolsCall_ToolWithSchema(t *testing.T) {
	schemaJSON := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	tool := &stubTool{
		name:      "schema_tool",
		desc:      "tool with schema",
		schema:    schemaJSON,
		invokeOut: json.RawMessage(`{"result":"ok"}`),
	}
	reg := tools.NewRegistry(tool)
	s := New(reg, Config{})

	// First list tools to exercise schema marshaling.
	listBody := `{"jsonrpc":"2.0","method":"tools/list","id":1}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(listBody))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("list error: %+v", resp.Error)
	}

	// Then call it.
	callBody := `{"jsonrpc":"2.0","method":"tools/call","id":2,"params":{"name":"schema_tool","arguments":{"name":"test"}}}`
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(callBody))
	s.handleMessage(rr2, req2)

	var resp2 mcp.Response
	json.Unmarshal(rr2.Body.Bytes(), &resp2)
	if resp2.Error != nil {
		t.Fatalf("call error: %+v", resp2.Error)
	}
}

// ---------------------------------------------------------------------------
// handleToolsList — tool with nil schema
// ---------------------------------------------------------------------------

func TestHandleToolsList_NilSchema(t *testing.T) {
	tool := &stubTool{
		name:   "no_schema_tool",
		desc:   "tool without schema",
		schema: nil,
	}
	reg := tools.NewRegistry(tool)
	s := New(reg, Config{})

	body := `{"jsonrpc":"2.0","method":"tools/list","id":1}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}
