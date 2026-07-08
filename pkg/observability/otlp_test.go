// otlp_test.go — exercises the real OTLP exporter against an httptest
// server pretending to be an OTLP/HTTP collector. Build with
// `go test -tags otel ./pkg/observability/...`.

//go:build otel

package observability

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// fakeCollector is a minimal stand-in for an OTLP/HTTP collector. It
// accepts POSTs to /v1/traces, decodes the protobuf, and remembers every
// span it sees so the test can assert on them.
type fakeCollector struct {
	mu     sync.Mutex
	spans  []*tracepb.Span
	server *httptest.Server
}

func newFakeCollector(t *testing.T) *fakeCollector {
	t.Helper()
	fc := &fakeCollector{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Header.Get("Content-Encoding") == "gzip" {
			gz, gerr := gzip.NewReader(bytes.NewReader(body))
			if gerr != nil {
				http.Error(w, gerr.Error(), http.StatusBadRequest)
				return
			}
			body, err = io.ReadAll(gz)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		var req collectortracepb.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fc.mu.Lock()
		for _, rs := range req.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				fc.spans = append(fc.spans, ss.Spans...)
			}
		}
		fc.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		resp := &collectortracepb.ExportTraceServiceResponse{}
		out, _ := proto.Marshal(resp)
		_, _ = w.Write(out)
	})
	fc.server = httptest.NewServer(mux)
	return fc
}

func (fc *fakeCollector) snapshot() []*tracepb.Span {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	out := make([]*tracepb.Span, len(fc.spans))
	copy(out, fc.spans)
	return out
}

func TestOTLPLLMExporter_ExportsSpansAndShutdownFlushes(t *testing.T) {
	fc := newFakeCollector(t)
	defer fc.server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Endpoint is "http://127.0.0.1:PORT" — the exporter strips the scheme
	// and uses Insecure for http.
	u, err := url.Parse(fc.server.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	endpoint := "http://" + u.Host

	exp, err := NewOTLPLLMExporter(ctx, endpoint)
	if err != nil {
		t.Fatalf("NewOTLPLLMExporter: %v", err)
	}

	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	traces := []LLMTrace{
		{
			StartedAt: start, DurationMs: 120,
			Provider: "anthropic", Model: "claude-opus-4-7", AgentID: "diagnostician",
			CaseID: "MRN-12345", TraceID: "trace-a",
			InputTokens: 100, OutputTokens: 50, CachedTokens: 10,
			StopReason: "end_turn", ToolCalls: 1, Kind: "completion",
		},
		{
			StartedAt: start.Add(time.Second), DurationMs: 80,
			Provider: "openai", Model: "gpt-4o", AgentID: "intake",
			CaseID: "MRN-99999", TraceID: "trace-b",
			InputTokens: 50, OutputTokens: 20,
			Kind: "completion",
		},
		{
			StartedAt: start.Add(2 * time.Second), DurationMs: 15,
			Provider: "ollama", Model: "nomic-embed-text", AgentID: "rag",
			InputTokens: 200,
			Kind:        "embedding",
		},
	}
	for _, tr := range traces {
		exp.Record(tr)
	}

	// Shutdown must flush pending spans before returning.
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	got := fc.snapshot()
	if len(got) != 3 {
		t.Fatalf("expected 3 spans after Shutdown flush, got %d", len(got))
	}

	// Index by agent_id attribute so we can assert per-trace specifics
	// regardless of batching order.
	byAgent := map[string]*tracepb.Span{}
	for _, s := range got {
		for _, a := range s.Attributes {
			if a.Key == "agent_id" {
				byAgent[a.Value.GetStringValue()] = s
			}
		}
	}

	diag, ok := byAgent["diagnostician"]
	if !ok {
		t.Fatalf("missing span for diagnostician; agents seen: %v", keys(byAgent))
	}
	if diag.Name != "llm.completion" {
		t.Errorf("diagnostician span name = %q, want llm.completion", diag.Name)
	}
	attrs := attrMap(diag.Attributes)
	if attrs["provider"] != "anthropic" {
		t.Errorf("provider = %q, want anthropic", attrs["provider"])
	}
	if attrs["model"] != "claude-opus-4-7" {
		t.Errorf("model = %q, want claude-opus-4-7", attrs["model"])
	}
	if attrs["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", attrs["stop_reason"])
	}
	if attrs["kind"] != "completion" {
		t.Errorf("kind = %q, want completion", attrs["kind"])
	}
	// PHI: CaseID MUST be hashed, never the raw MRN.
	if attrs["case_id"] == "MRN-12345" {
		t.Errorf("raw MRN leaked into span attribute: %q", attrs["case_id"])
	}
	if attrs["case_id"] == "" {
		t.Errorf("case_id missing entirely; expected hashed value")
	}
	if strings.Contains(attrs["case_id"], "MRN") {
		t.Errorf("case_id contains MRN substring: %q", attrs["case_id"])
	}
	// Duration: span end - start == 120ms.
	gotDur := time.Duration(diag.EndTimeUnixNano-diag.StartTimeUnixNano) * time.Nanosecond
	if gotDur != 120*time.Millisecond {
		t.Errorf("duration = %v, want 120ms", gotDur)
	}

	emb, ok := byAgent["rag"]
	if !ok {
		t.Fatalf("missing embedding span")
	}
	if emb.Name != "llm.embedding" {
		t.Errorf("embedding span name = %q, want llm.embedding", emb.Name)
	}
	embAttrs := attrMap(emb.Attributes)
	if embAttrs["kind"] != "embedding" {
		t.Errorf("kind = %q, want embedding", embAttrs["kind"])
	}
}

func TestOTLPLLMExporter_NilSafe(t *testing.T) {
	var e *OTLPLLMExporter
	// Should not panic.
	e.Record(LLMTrace{Provider: "p", Model: "m"})
	if err := e.Shutdown(context.Background()); err != nil {
		t.Errorf("nil Shutdown returned err: %v", err)
	}
}

// attrMap flattens a span's KeyValue list into a map[string]string for
// easy lookup in assertions. Int values are stringified.
func attrMap(kvs []*commonpb.KeyValue) map[string]string {
	out := map[string]string{}
	for _, kv := range kvs {
		out[kv.Key] = kv.Value.GetStringValue()
	}
	return out
}

func keys(m map[string]*tracepb.Span) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
