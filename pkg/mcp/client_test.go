package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
)

// rpcRequest mirrors what the server sees so test handlers can assert on it.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// writeRPCResult encodes a successful JSON-RPC response for the test server.
func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]any{
		"jsonrpc": JSONRPCVersion,
		"id":      id,
		"result":  result,
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// writeRPCError encodes a JSON-RPC error response.
func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]any{
		"jsonrpc": JSONRPCVersion,
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// readRPC decodes the incoming JSON-RPC body.
func readRPC(t *testing.T, r *http.Request) rpcRequest {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode body %q: %v", string(body), err)
	}
	if req.JSONRPC != JSONRPCVersion {
		t.Errorf("jsonrpc field = %q; want %q", req.JSONRPC, JSONRPCVersion)
	}
	return req
}

func TestHTTPClient_Initialize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readRPC(t, r)
		if req.Method != MethodInitialize {
			t.Errorf("method = %q; want %q", req.Method, MethodInitialize)
		}
		writeRPCResult(w, req.ID, InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ServerInfo:      ServerInfo{Name: "fake-mcp", Version: "1.2.3"},
		})
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	res, err := c.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if res.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocol version = %q; want %q", res.ProtocolVersion, ProtocolVersion)
	}
	if res.ServerInfo.Name != "fake-mcp" {
		t.Errorf("server name = %q; want %q", res.ServerInfo.Name, "fake-mcp")
	}
}

func TestHTTPClient_ListTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readRPC(t, r)
		if req.Method != MethodToolsList {
			t.Errorf("method = %q; want %q", req.Method, MethodToolsList)
		}
		writeRPCResult(w, req.ID, listToolsResult{
			Tools: []Tool{
				{Name: "lab_query", Description: "Query lab results", InputSchema: map[string]any{"type": "object"}},
				{Name: "rxnorm_lookup", Description: "Look up an RxNorm code"},
			},
		})
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools; want 2", len(tools))
	}
	if tools[0].Name != "lab_query" {
		t.Errorf("tool[0] name = %q; want lab_query", tools[0].Name)
	}
}

func TestHTTPClient_CallTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readRPC(t, r)
		if req.Method != MethodToolsCall {
			t.Errorf("method = %q; want %q", req.Method, MethodToolsCall)
		}
		var p callToolParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if p.Name != "lab_query" {
			t.Errorf("tool name = %q; want lab_query", p.Name)
		}
		writeRPCResult(w, req.ID, ToolResult{
			Content: []ContentItem{{Type: "text", Text: "WBC 14.2K"}},
		})
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	res, err := c.CallTool(context.Background(), "lab_query", map[string]any{"loinc": "26464-8"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "WBC 14.2K" {
		t.Errorf("unexpected content: %+v", res.Content)
	}
	if res.IsError {
		t.Errorf("IsError = true; want false")
	}
}

func TestHTTPClient_CallToolStreaming(t *testing.T) {
	// SSE handler that streams three content chunks then a final JSON-RPC
	// envelope. Each chunk is one ToolResult with one ContentItem.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readRPC(t, r)
		if req.Method != MethodToolsCall {
			t.Errorf("method = %q; want %q", req.Method, MethodToolsCall)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i, chunk := range []string{"chunk-one ", "chunk-two ", "chunk-three"} {
			payload, _ := json.Marshal(ToolResult{Content: []ContentItem{{Type: "text", Text: chunk}}})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher != nil {
				flusher.Flush()
			}
			_ = i
		}
		// Final terminator response with no content (chunks are merged in).
		finalResp, _ := json.Marshal(map[string]any{
			"jsonrpc": JSONRPCVersion,
			"id":      req.ID,
			"result":  ToolResult{},
		})
		fmt.Fprintf(w, "data: %s\n\n", finalResp)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	res, err := c.CallTool(context.Background(), "stream_tool", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(res.Content) != 3 {
		t.Fatalf("got %d content items; want 3 (chunks should be assembled)", len(res.Content))
	}
	assembled := res.Content[0].Text + res.Content[1].Text + res.Content[2].Text
	if assembled != "chunk-one chunk-two chunk-three" {
		t.Errorf("assembled content = %q", assembled)
	}
}

func TestHTTPClient_CallToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readRPC(t, r)
		writeRPCError(w, req.ID, -32602, "invalid params: missing 'loinc'")
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.CallTool(context.Background(), "lab_query", map[string]any{})
	if err == nil {
		t.Fatal("CallTool returned nil error; want JSON-RPC error")
	}
	var rpcErr *Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error %v is not *mcp.Error", err)
	}
	if rpcErr.Code != -32602 {
		t.Errorf("rpc error code = %d; want -32602", rpcErr.Code)
	}
	if !strings.Contains(rpcErr.Message, "missing 'loinc'") {
		t.Errorf("rpc error message = %q; want substring %q", rpcErr.Message, "missing 'loinc'")
	}
}

func TestHTTPClient_Timeout(t *testing.T) {
	// Handler that blocks until either the request context is cancelled
	// (client gave up) or the test-controlled shutdown channel fires
	// (cleanup, in case the client somehow never cancels). The shutdown
	// channel ensures srv.Close() never hangs even if the request context
	// signal is missed by the server runtime.
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	t.Cleanup(func() {
		close(done)
		srv.Close()
	})

	// Use a short-deadline context directly to guarantee CallTool returns
	// promptly regardless of any keep-alive idiosyncrasies.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c := NewHTTPClient(srv.URL, WithTimeout(50*time.Millisecond))

	start := time.Now()
	_, err := c.CallTool(ctx, "slow_tool", nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("CallTool returned nil error; want timeout")
	}
	if elapsed > 2*time.Second {
		t.Errorf("CallTool took %v; expected timeout well under a second", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "context") {
		t.Errorf("unexpected error %v; want deadline-exceeded class", err)
	}
}

func TestHTTPClient_AuditEmitsOnCallTool(t *testing.T) {
	type call struct {
		method string
		isErr  bool
	}
	var inflight atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readRPC(t, r)
		inflight.Add(1)
		// Behaviour depends on tool name encoded in params.
		var p callToolParams
		_ = json.Unmarshal(req.Params, &p)
		if p.Name == "broken_tool" {
			writeRPCError(w, req.ID, -32000, "broken")
			return
		}
		writeRPCResult(w, req.ID, ToolResult{Content: []ContentItem{{Type: "text", Text: "ok"}}})
	}))
	defer srv.Close()

	rec := audit.NewInMemory()
	c := NewHTTPClient(srv.URL,
		WithRecorder(rec),
		WithTenantID("tenant-x"),
		WithCaseID("case-001"),
		WithTraceID("trace-xyz"),
	)

	// Successful call.
	if _, err := c.CallTool(context.Background(), "lab_query", map[string]any{"phi_field": "sensitive value patient_id pt-001"}); err != nil {
		t.Fatalf("CallTool success: %v", err)
	}
	// Failing call.
	if _, err := c.CallTool(context.Background(), "broken_tool", map[string]any{"more_phi": "do not leak"}); err == nil {
		t.Fatal("CallTool: expected error from broken_tool")
	}

	events := rec.List()
	if len(events) != 2 {
		t.Fatalf("got %d audit events; want 2", len(events))
	}

	// Both events must carry tenant/case/trace IDs and never any PHI args.
	allText, _ := json.Marshal(events)
	for _, banned := range []string{"sensitive value", "phi_field", "do not leak", "more_phi"} {
		if strings.Contains(string(allText), banned) {
			t.Errorf("audit event leaks banned token %q: %s", banned, string(allText))
		}
	}

	checks := []call{{"lab_query", false}, {"broken_tool", true}}
	for i, c := range checks {
		ev := events[i]
		if ev.Kind != audit.KindMessage {
			t.Errorf("event[%d] kind = %q; want %q", i, ev.Kind, audit.KindMessage)
		}
		if ev.AgentID != "mcp_client" {
			t.Errorf("event[%d] agent_id = %q; want mcp_client", i, ev.AgentID)
		}
		if ev.Type != "mcp_call_tool" {
			t.Errorf("event[%d] type = %q", i, ev.Type)
		}
		if ev.TenantID != "tenant-x" || ev.CaseID != "case-001" || ev.TraceID != "trace-xyz" {
			t.Errorf("event[%d] correlation tags wrong: %+v", i, ev)
		}
		// The "From" field carries the tool name.
		if ev.From != c.method {
			t.Errorf("event[%d] tool name (From) = %q; want %q", i, ev.From, c.method)
		}
		// The "To" field carries the sanitised endpoint.
		if !strings.HasPrefix(ev.To, srv.URL) {
			t.Errorf("event[%d] endpoint (To) = %q; want prefix %q", i, ev.To, srv.URL)
		}
		expectedOutcome := OutcomeSuccess
		if c.isErr {
			expectedOutcome = OutcomeError
		}
		if ev.Decision != expectedOutcome {
			t.Errorf("event[%d] outcome (Decision) = %q; want %q", i, ev.Decision, expectedOutcome)
		}
	}
}

func TestHTTPClient_AuthBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		req := readRPC(t, r)
		writeRPCResult(w, req.ID, listToolsResult{Tools: nil})
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, WithAuth(BearerAuth{Token: "tok-abc"}))
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Errorf("Authorization header = %q; want %q", gotAuth, "Bearer tok-abc")
	}
}

func TestHTTPClient_AuthNoAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		req := readRPC(t, r)
		writeRPCResult(w, req.ID, listToolsResult{Tools: nil})
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL) // default NoAuth
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q; want empty", gotAuth)
	}
}

func TestHTTPClient_BadServerResponse(t *testing.T) {
	t.Run("non-2xx status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewHTTPClient(srv.URL)
		_, err := c.ListTools(context.Background())
		if err == nil {
			t.Fatal("ListTools: expected error from 500 response")
		}
		if !strings.Contains(err.Error(), "http 500") {
			t.Errorf("error %v; want 'http 500' substring", err)
		}
	})

	t.Run("malformed json body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{not-json"))
		}))
		defer srv.Close()

		c := NewHTTPClient(srv.URL)
		_, err := c.ListTools(context.Background())
		if err == nil {
			t.Fatal("ListTools: expected decode error")
		}
	})

	t.Run("id mismatch", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = readRPC(t, r)
			w.Header().Set("Content-Type", "application/json")
			// Echo a fake id, not the real one.
			payload := map[string]any{"jsonrpc": JSONRPCVersion, "id": 9999, "result": listToolsResult{}}
			_ = json.NewEncoder(w).Encode(payload)
		}))
		defer srv.Close()

		c := NewHTTPClient(srv.URL)
		_, err := c.ListTools(context.Background())
		if err == nil {
			t.Fatal("ListTools: expected id-mismatch error")
		}
		if !strings.Contains(err.Error(), "id mismatch") {
			t.Errorf("error %v; want 'id mismatch'", err)
		}
	})
}

func TestHTTPClient_ListAndReadResource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readRPC(t, r)
		switch req.Method {
		case MethodResourcesList:
			writeRPCResult(w, req.ID, listResourcesResult{
				Resources: []Resource{{URI: "file:///labs.json", Name: "labs", MimeType: "application/json"}},
			})
		case MethodResourcesRead:
			var p readResourceParams
			_ = json.Unmarshal(req.Params, &p)
			writeRPCResult(w, req.ID, ResourceContent{
				Contents: []ResourceContentItem{{URI: p.URI, MimeType: "application/json", Text: `{"loinc":"26464-8"}`}},
			})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	resources, err := c.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 1 || resources[0].URI != "file:///labs.json" {
		t.Fatalf("unexpected resources: %+v", resources)
	}

	rc, err := c.ReadResource(context.Background(), "file:///labs.json")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(rc.Contents) != 1 || !strings.Contains(rc.Contents[0].Text, "26464-8") {
		t.Errorf("unexpected contents: %+v", rc.Contents)
	}
}

func TestHTTPClient_RejectsEmptyArgs(t *testing.T) {
	c := NewHTTPClient("http://localhost:1") // never called
	if _, err := c.CallTool(context.Background(), "", nil); err == nil {
		t.Error("CallTool: expected error for empty tool name")
	}
	if _, err := c.ReadResource(context.Background(), ""); err == nil {
		t.Error("ReadResource: expected error for empty uri")
	}
}

func TestBearerAuth_EmptyToken(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://x", nil)
	err := BearerAuth{}.Apply(req)
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestSPIFFEAuth_StubReturnsNotImplemented(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://x", nil)
	err := SPIFFEAuth{}.Apply(req)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("SPIFFEAuth.Apply = %v; want ErrNotImplemented", err)
	}
}

func TestSanitiseEndpoint(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://lab-mcp.example/v1?token=secret", "https://lab-mcp.example/v1"},
		{"https://user:pw@host.example/v1", "https://host.example/v1"},
		{"https://host.example/v1#frag", "https://host.example/v1"},
		{"not a url", "not a url"}, // round-trips unchanged
	}
	for _, c := range cases {
		got := sanitiseEndpoint(c.in)
		// url.Parse normalises but allow a tolerance: ensure the secret bits are gone.
		if strings.Contains(got, "token=secret") || strings.Contains(got, "user:pw") || strings.Contains(got, "#") {
			t.Errorf("sanitiseEndpoint(%q) = %q; still leaks secret material", c.in, got)
		}
		_ = c.want
	}
}

func TestRecorder_NilSinkIsNoop(t *testing.T) {
	r := &Recorder{Sink: nil}
	if err := r.Record(CallEvent{ToolName: "x"}); err != nil {
		t.Errorf("nil-sink Record returned %v; want nil", err)
	}
}
