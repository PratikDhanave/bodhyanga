package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/mcp"
	"github.com/PratikDhanave/bodh/pkg/tools"
)

// ---- Config defaults ----

func TestNew_DefaultConfig(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})
	if s.name != DefaultName {
		t.Errorf("name = %q, want %q", s.name, DefaultName)
	}
	if s.version != DefaultVersion {
		t.Errorf("version = %q, want %q", s.version, DefaultVersion)
	}
}

func TestNew_CustomConfig(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{Addr: ":9999", Name: "custom", Version: "1.0"})
	if s.name != "custom" {
		t.Errorf("name = %q, want custom", s.name)
	}
	if s.version != "1.0" {
		t.Errorf("version = %q, want 1.0", s.version)
	}
}

// ---- SSE ----

func TestHandleSSE_MethodNotAllowed(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sse", nil)
	s.handleSSE(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

// ---- handleMessage ----

func TestHandleMessage_MethodNotAllowed(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/message", nil)
	s.handleMessage(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandleMessage_InvalidJSON(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader("not json"))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeParseError {
		t.Errorf("expected parse error, got %+v", resp.Error)
	}
}

func TestHandleMessage_WrongJSONRPCVersion(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	body := `{"jsonrpc":"1.0","method":"initialize","id":1}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeInvalidRequest {
		t.Errorf("expected invalid request, got %+v", resp.Error)
	}
}

func TestHandleMessage_UnknownMethod(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	body := `{"jsonrpc":"2.0","method":"unknown/method","id":1}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeMethodNotFound {
		t.Errorf("expected method not found, got %+v", resp.Error)
	}
}

// ---- tools/call ----

func TestHandleToolsCall_EmptyName(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	body := `{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"","arguments":{}}}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeInvalidParams {
		t.Errorf("expected invalid params, got %+v", resp.Error)
	}
}

func TestHandleToolsCall_InvalidParams(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	body := `{"jsonrpc":"2.0","method":"tools/call","id":1,"params":"not an object"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeInvalidParams {
		t.Errorf("expected invalid params, got %+v", resp.Error)
	}
}

func TestHandleToolsCall_ToolNotFound(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	body := `{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"nonexistent","arguments":{}}}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeMethodNotFound {
		t.Errorf("expected method not found, got %+v", resp.Error)
	}
}

func TestHandleToolsCall_InvokeError(t *testing.T) {
	errTool := &stubTool{
		name:      "fail_tool",
		desc:      "a tool that fails",
		invokeErr: context.Canceled,
	}
	reg := tools.NewRegistry(errTool)
	s := New(reg, Config{})

	body := `{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"fail_tool","arguments":{}}}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("transport error should be nil for invoke error: %+v", resp.Error)
	}

	var tr mcp.ToolResult
	json.Unmarshal(resp.Result, &tr)
	if !tr.IsError {
		t.Error("expected ToolResult.IsError = true")
	}
}

func TestHandleToolsCall_Success(t *testing.T) {
	okTool := &stubTool{
		name:      "ok_tool",
		desc:      "a tool that succeeds",
		invokeOut: json.RawMessage(`{"result":"ok"}`),
	}
	reg := tools.NewRegistry(okTool)
	s := New(reg, Config{})

	body := `{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"ok_tool","arguments":{"key":"val"}}}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	var tr mcp.ToolResult
	json.Unmarshal(resp.Result, &tr)
	if tr.IsError {
		t.Error("expected ToolResult.IsError = false")
	}
}

func TestHandleToolsCall_NoArguments(t *testing.T) {
	okTool := &stubTool{
		name:      "ok_tool",
		desc:      "a tool that succeeds",
		invokeOut: json.RawMessage(`{"result":"ok"}`),
	}
	reg := tools.NewRegistry(okTool)
	s := New(reg, Config{})

	// No arguments field.
	body := `{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"ok_tool"}}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

// ---- tools/list with schema unmarshal error ----

func TestHandleToolsList_BadSchema(t *testing.T) {
	badTool := &stubTool{
		name:   "bad_schema",
		desc:   "tool with invalid schema",
		schema: json.RawMessage(`not json`),
	}
	reg := tools.NewRegistry(badTool)
	s := New(reg, Config{})

	body := `{"jsonrpc":"2.0","method":"tools/list","id":1}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body))
	s.handleMessage(rr, req)

	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	// Should succeed with nil schema, not fail.
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

// ---- writeJSON marshal error ----

func TestWriteJSON_MarshalError(t *testing.T) {
	rr := httptest.NewRecorder()
	// Pass an unmarshallable value.
	writeJSON(rr, http.StatusOK, make(chan int))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

// ---- internalError ----

func TestInternalError(t *testing.T) {
	e := internalError("test error")
	if e.Code != errCodeInternalError {
		t.Errorf("code = %d, want %d", e.Code, errCodeInternalError)
	}
	if e.Message != "test error" {
		t.Errorf("message = %q, want 'test error'", e.Message)
	}
}

// ---- Handler() method ----

func TestHandler(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})
	h := s.Handler()
	if h == nil {
		t.Error("Handler() should not return nil")
	}
}

// ---- handleSSE success path ----

func TestHandleSSE_Success(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	// Use a context that cancels quickly to let the SSE handler return.
	ctx, cancel := context.WithCancel(context.Background())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		s.handleSSE(rr, req)
		close(done)
	}()

	// Give it a moment to write the endpoint event, then cancel.
	cancel()
	<-done

	if !strings.Contains(rr.Body.String(), "endpoint") {
		t.Errorf("expected endpoint event, got %q", rr.Body.String())
	}
}

// ---- handleMessage with read error ----

func TestHandleMessage_ReadError(t *testing.T) {
	reg := tools.NewRegistry()
	s := New(reg, Config{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/message", &errReader{})
	s.handleMessage(rr, req)

	// Should get a parse error for read failure.
	var resp mcp.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	// The response should be some error.
	if rr.Body.Len() == 0 {
		t.Error("expected non-empty response")
	}
}

// errReader always returns an error on Read.
type errReader struct{}

func (e *errReader) Read(_ []byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// stubTool implements tools.Tool for testing.
type stubTool struct {
	name      string
	desc      string
	schema    json.RawMessage
	invokeOut json.RawMessage
	invokeErr error
}

func (s *stubTool) Name() string                                             { return s.name }
func (s *stubTool) Description() string                                      { return s.desc }
func (s *stubTool) Schema() json.RawMessage                                  { return s.schema }
func (s *stubTool) Invoke(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	return s.invokeOut, s.invokeErr
}
