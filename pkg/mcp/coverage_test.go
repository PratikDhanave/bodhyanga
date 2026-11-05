package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
)

// ---------------------------------------------------------------------------
// Client Option tests
// ---------------------------------------------------------------------------

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 99 * time.Second}
	c := NewHTTPClient("http://localhost", WithHTTPClient(custom))
	if c.transport.http != custom {
		t.Fatal("expected custom http client")
	}

	// nil should be ignored.
	c2 := NewHTTPClient("http://localhost", WithHTTPClient(nil))
	if c2.transport.http == nil {
		t.Fatal("nil http client should not be set")
	}
}

func TestWithProtocolVersion(t *testing.T) {
	c := NewHTTPClient("http://localhost", WithProtocolVersion("2025-01-01"))
	if c.protocolVersion != "2025-01-01" {
		t.Fatalf("expected 2025-01-01, got %q", c.protocolVersion)
	}

	// Empty should keep default.
	c2 := NewHTTPClient("http://localhost", WithProtocolVersion(""))
	if c2.protocolVersion != ProtocolVersion {
		t.Fatalf("expected default version, got %q", c2.protocolVersion)
	}
}

func TestWithClientInfo(t *testing.T) {
	c := NewHTTPClient("http://localhost", WithClientInfo("my-client", "1.0"))
	if c.clientName != "my-client" {
		t.Fatalf("expected my-client, got %q", c.clientName)
	}
	if c.clientVersion != "1.0" {
		t.Fatalf("expected 1.0, got %q", c.clientVersion)
	}

	// Empty values should keep defaults.
	c2 := NewHTTPClient("http://localhost", WithClientInfo("", ""))
	if c2.clientName != DefaultClientName {
		t.Fatalf("expected default client name, got %q", c2.clientName)
	}
}

func TestWithAuth_Nil(t *testing.T) {
	c := NewHTTPClient("http://localhost", WithAuth(nil))
	// Should set NoAuth, not nil.
	if c.transport.auth == nil {
		t.Fatal("expected non-nil auth after WithAuth(nil)")
	}
}

func TestWithTimeout_Zero(t *testing.T) {
	c := NewHTTPClient("http://localhost", WithTimeout(0))
	if c.timeout != DefaultCallTimeout {
		t.Fatalf("expected default timeout, got %v", c.timeout)
	}
}

func TestWithTimeout_Negative(t *testing.T) {
	c := NewHTTPClient("http://localhost", WithTimeout(-5*time.Second))
	if c.timeout != DefaultCallTimeout {
		t.Fatalf("expected default timeout, got %v", c.timeout)
	}
}

func TestClose(t *testing.T) {
	c := NewHTTPClient("http://localhost")
	if err := c.Close(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// CallTool tests
// ---------------------------------------------------------------------------

func TestCallTool_EmptyName(t *testing.T) {
	c := NewHTTPClient("http://localhost")
	_, err := c.CallTool(context.Background(), "", nil)
	if err == nil || !strings.Contains(err.Error(), "tool name is empty") {
		t.Fatalf("expected empty name error, got: %v", err)
	}
}

func TestCallTool_TransportError(t *testing.T) {
	// Use an unreachable endpoint.
	c := NewHTTPClient("http://127.0.0.1:1")
	_, err := c.CallTool(context.Background(), "test-tool", nil)
	if err == nil {
		t.Fatal("expected transport error")
	}
}

func TestCallTool_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := Response{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage("1"),
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"hello"}],"isError":false}`),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	result, err := c.CallTool(context.Background(), "greet", map[string]any{"name": "world"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("expected no error")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
}

func TestCallTool_ToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := Response{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage("1"),
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"fail"}],"isError":true}`),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	result, err := c.CallTool(context.Background(), "fail-tool", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected isError=true")
	}
}

func TestCallTool_JSONRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := Response{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage("1"),
			Error:   &Error{Code: -32601, Message: "method not found"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.CallTool(context.Background(), "missing", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var mcpErr *Error
	if !errors.As(err, &mcpErr) {
		t.Fatal("expected *mcp.Error")
	}
	if mcpErr.Code != -32601 {
		t.Fatalf("expected code -32601, got %d", mcpErr.Code)
	}
}

func TestCallTool_WithRecorder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := Response{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage("1"),
			Result:  json.RawMessage(`{"content":[]}`),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rec := audit.NewInMemory()
	c := NewHTTPClient(srv.URL,
		WithRecorder(rec),
		WithTenantID("t1"),
		WithCaseID("c1"),
		WithTraceID("tr1"),
	)
	c.CallTool(context.Background(), "tool", nil)

	events := rec.List()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	if events[0].TenantID != "t1" {
		t.Fatalf("expected tenant t1, got %q", events[0].TenantID)
	}
}

// ---------------------------------------------------------------------------
// ReadResource tests
// ---------------------------------------------------------------------------

func TestReadResource_EmptyURI(t *testing.T) {
	c := NewHTTPClient("http://localhost")
	_, err := c.ReadResource(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "uri is empty") {
		t.Fatalf("expected empty uri error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Transport + SSE tests
// ---------------------------------------------------------------------------

func TestTransport_HTTP400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected 400 in error, got: %v", err)
	}
}

func TestTransport_SSEStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		resp := Response{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage("1"),
			Result:  json.RawMessage(`{"tools":[]}`),
		}
		data, _ := json.Marshal(resp)
		w.Write([]byte("data: " + string(data) + "\n\n"))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tools == nil {
		t.Fatal("expected non-nil tools list")
	}
}

func TestTransport_SSEPartialChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		// Send partial ToolResult chunks then a final response.
		w.Write([]byte("data: {\"content\":[{\"type\":\"text\",\"text\":\"chunk1\"}]}\n\n"))
		resp := Response{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage("1"),
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"final"}]}`),
		}
		data, _ := json.Marshal(resp)
		w.Write([]byte("data: " + string(data) + "\n\n"))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	result, err := c.CallTool(context.Background(), "stream-tool", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The accumulated chunks should override the final response.
	if len(result.Content) == 0 {
		t.Fatal("expected content from accumulated chunks")
	}
}

func TestTransport_SSENoPayloads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(": comment\nevent: something\nid: 123\nretry: 5000\n\n"))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no payloads") {
		t.Fatalf("expected no payloads error, got: %v", err)
	}
}

func TestTransport_SSEErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		resp := Response{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage("1"),
			Error:   &Error{Code: -32603, Message: "internal"},
		}
		data, _ := json.Marshal(resp)
		w.Write([]byte("data: " + string(data) + "\n\n"))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error from SSE error response")
	}
}

func TestTransport_SSEBareContentItem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("data: {\"type\":\"text\",\"text\":\"bare item\"}\n\n"))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	result, err := c.CallTool(context.Background(), "bare-tool", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "bare item" {
		t.Fatalf("expected bare content item, got: %+v", result)
	}
}

func TestTransport_SSEUnrecognisedPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("data: {\"unknown_field\":true}\n\n"))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.CallTool(context.Background(), "unknown-tool", nil)
	if err == nil || !strings.Contains(err.Error(), "unrecognised") {
		t.Fatalf("expected unrecognised payload error, got: %v", err)
	}
}

func TestTransport_ReadJSON_IDMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := Response{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage("999"),
			Result:  json.RawMessage(`{}`),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.Initialize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "id mismatch") {
		t.Fatalf("expected id mismatch error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// decodeResult tests
// ---------------------------------------------------------------------------

func TestDecodeResult_NilResponse(t *testing.T) {
	err := decodeResult(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "nil response") {
		t.Fatalf("expected nil response error, got: %v", err)
	}
}

func TestDecodeResult_WithError(t *testing.T) {
	resp := &Response{Error: &Error{Code: -32600, Message: "invalid"}}
	err := decodeResult(resp, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var mcpErr *Error
	if !errors.As(err, &mcpErr) {
		t.Fatal("expected *mcp.Error")
	}
}

func TestDecodeResult_NilTarget(t *testing.T) {
	resp := &Response{Result: json.RawMessage(`{}`)}
	err := decodeResult(resp, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeResult_EmptyResult(t *testing.T) {
	resp := &Response{}
	var out struct{}
	err := decodeResult(resp, &out)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeResult_BadJSON(t *testing.T) {
	resp := &Response{Result: json.RawMessage(`not json`)}
	var out struct{}
	err := decodeResult(resp, &out)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

// ---------------------------------------------------------------------------
// Audit helpers tests
// ---------------------------------------------------------------------------

func TestSanitiseEndpoint_Coverage(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"https://user:pass@example.com/path?key=val", "https://example.com/path"},
		{"http://localhost:8080/mcp", "http://localhost:8080/mcp"},
		{"://invalid", "://invalid"}, // invalid URL passes through
	}
	for _, tt := range tests {
		got := sanitiseEndpoint(tt.input)
		if got != tt.want {
			t.Errorf("sanitiseEndpoint(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	if formatDuration(0) != "" {
		t.Fatal("expected empty for zero duration")
	}
	if formatDuration(-1) != "" {
		t.Fatal("expected empty for negative duration")
	}
	d := formatDuration(1500 * time.Millisecond)
	if d == "" {
		t.Fatal("expected non-empty for positive duration")
	}
}

func TestTruncateForLog(t *testing.T) {
	short := "hello"
	if truncateForLog(short) != short {
		t.Fatal("short string should not be truncated")
	}

	long := strings.Repeat("x", 300)
	result := truncateForLog(long)
	if !strings.HasSuffix(result, "...(truncated)") {
		t.Fatal("expected truncation suffix")
	}
	if len(result) > 260 {
		t.Fatal("truncated string too long")
	}
}

// ---------------------------------------------------------------------------
// classifyOutcome tests
// ---------------------------------------------------------------------------

func TestClassifyOutcome(t *testing.T) {
	if classifyOutcome(context.Background(), nil) != OutcomeSuccess {
		t.Fatal("expected success for nil error")
	}
	if classifyOutcome(context.Background(), errors.New("fail")) != OutcomeError {
		t.Fatal("expected error for generic error")
	}
	if classifyOutcome(context.Background(), context.DeadlineExceeded) != OutcomeTimeout {
		t.Fatal("expected timeout for DeadlineExceeded")
	}
	if classifyOutcome(context.Background(), context.Canceled) != OutcomeTimeout {
		t.Fatal("expected timeout for Canceled")
	}

	// Test with cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if classifyOutcome(ctx, errors.New("some error")) != OutcomeTimeout {
		t.Fatal("expected timeout when context is cancelled")
	}
}

// ---------------------------------------------------------------------------
// Error type tests
// ---------------------------------------------------------------------------

func TestError_ErrorNil(t *testing.T) {
	var e *Error
	if e.Error() != "" {
		t.Fatalf("expected empty string for nil Error, got %q", e.Error())
	}
}

func TestError_ErrorNonNil(t *testing.T) {
	e := &Error{Code: -32601, Message: "method not found"}
	s := e.Error()
	if !strings.Contains(s, "-32601") || !strings.Contains(s, "method not found") {
		t.Fatalf("unexpected error string: %s", s)
	}
}

// ---------------------------------------------------------------------------
// RecordCall with nil recorder
// ---------------------------------------------------------------------------

func TestRecordCall_NilRecorder(t *testing.T) {
	c := &HTTPClient{}
	c.recorder = nil
	// Should not panic.
	c.recordCall("tool", time.Second, OutcomeSuccess, "")
}

func TestRecorder_Record_NilSink(t *testing.T) {
	r := &Recorder{Sink: nil}
	err := r.Record(CallEvent{ToolName: "test"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRecorder_Record_NilReceiver(t *testing.T) {
	var r *Recorder
	err := r.Record(CallEvent{ToolName: "test"})
	if err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// withDeadline tests
// ---------------------------------------------------------------------------

func TestWithDeadline_ExistingDeadline(t *testing.T) {
	c := NewHTTPClient("http://localhost", WithTimeout(time.Hour))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ctx2, cancel2 := c.withDeadline(ctx)
	defer cancel2()

	// Should respect the caller's deadline, not add a new one.
	deadline, ok := ctx2.Deadline()
	if !ok {
		t.Fatal("expected deadline")
	}
	// The deadline should be ~5s from now, not 1h.
	if time.Until(deadline) > 10*time.Second {
		t.Fatal("expected caller's deadline to be respected")
	}
}

func TestWithDeadline_NoDeadlineNoTimeout(t *testing.T) {
	c := &HTTPClient{timeout: 0}
	ctx, cancel := c.withDeadline(context.Background())
	defer cancel()

	_, ok := ctx.Deadline()
	if ok {
		t.Fatal("expected no deadline when timeout is 0")
	}
}
