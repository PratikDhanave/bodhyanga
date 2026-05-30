// laminar_test.go — exercises the LaminarExporter against an httptest server
// that pretends to be an OTLP/HTTP collector. Build with
// `go test -tags otel ./pkg/observability/...`.

//go:build otel

package observability

import (
	"context"
	"strings"
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
)

// fakeCollector is already defined in otlp_test.go (same package, same tag).
// hashID is already defined in otlp.go.
// attrMap is already defined in otlp_test.go.

// ── helper: build a LaminarExporter against a fakeCollector ──────────────────

func newLaminarExporterForTest(t *testing.T) (*LaminarExporter, *fakeCollector) {
	t.Helper()
	fc := newFakeCollector(t)
	t.Cleanup(fc.server.Close)

	// Override LMNR_BASE_URL to point at the fake collector.
	t.Setenv("LMNR_BASE_URL", "http://"+fc.server.Listener.Addr().String())
	// API key is optional for a fake collector — set a dummy value so the
	// header is present in assertions.
	t.Setenv("LMNR_PROJECT_API_KEY", "test-api-key")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	exp, err := NewLaminarExporter(ctx)
	if err != nil {
		t.Fatalf("NewLaminarExporter: %v", err)
	}
	return exp, fc
}

// ── LaminarExporter attribute tests ──────────────────────────────────────────

func TestLaminarExporter_SetsRequiredAttributes(t *testing.T) {
	exp, fc := newLaminarExporterForTest(t)

	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	exp.Record(LLMTrace{
		StartedAt:    start,
		DurationMs:   250,
		Provider:     "anthropic",
		Model:        "claude-haiku-4-5",
		AgentID:      "diagnostician",
		InputTokens:  200,
		OutputTokens: 80,
		CachedTokens: 30,
		StopReason:   "end_turn",
		ToolCalls:    2,
		Kind:         "completion",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	spans := fc.snapshot()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	s := spans[0]

	// Span name should be the agent ID.
	if s.Name != "diagnostician" {
		t.Errorf("span name = %q, want diagnostician", s.Name)
	}

	attrs := attrMap(s.Attributes)

	// Laminar recognition marker — must be present and exactly "LLM".
	if attrs[attrLaminarSpanType] != attrLaminarSpanLLM {
		t.Errorf("lmnr.span.type = %q, want %q", attrs[attrLaminarSpanType], attrLaminarSpanLLM)
	}

	// gen_ai.* semantic conventions.
	if attrs["gen_ai.system"] != "anthropic" {
		t.Errorf("gen_ai.system = %q, want anthropic", attrs["gen_ai.system"])
	}
	if attrs["gen_ai.request.model"] != "claude-haiku-4-5" {
		t.Errorf("gen_ai.request.model = %q, want claude-haiku-4-5", attrs["gen_ai.request.model"])
	}

	// Token attrs are stored as int; attrMap stringifies them via GetStringValue
	// which returns "" for int values. Check via the raw attribute list instead.
	intAttrs := intAttrMap(s.Attributes)
	if intAttrs["gen_ai.usage.input_tokens"] != 200 {
		t.Errorf("gen_ai.usage.input_tokens = %d, want 200", intAttrs["gen_ai.usage.input_tokens"])
	}
	if intAttrs["gen_ai.usage.output_tokens"] != 80 {
		t.Errorf("gen_ai.usage.output_tokens = %d, want 80", intAttrs["gen_ai.usage.output_tokens"])
	}
	if intAttrs["gen_ai.usage.cache_read_input_tokens"] != 30 {
		t.Errorf("gen_ai.usage.cache_read_input_tokens = %d, want 30", intAttrs["gen_ai.usage.cache_read_input_tokens"])
	}
	if intAttrs["bodh.tool_calls"] != 2 {
		t.Errorf("bodh.tool_calls = %d, want 2", intAttrs["bodh.tool_calls"])
	}

	// Optional attributes.
	if attrs["gen_ai.response.finish_reasons"] != "end_turn" {
		t.Errorf("gen_ai.response.finish_reasons = %q, want end_turn", attrs["gen_ai.response.finish_reasons"])
	}
	if attrs["bodh.agent_id"] != "diagnostician" {
		t.Errorf("bodh.agent_id = %q, want diagnostician", attrs["bodh.agent_id"])
	}
}

func TestLaminarExporter_ProviderLowercased(t *testing.T) {
	exp, fc := newLaminarExporterForTest(t)

	exp.Record(LLMTrace{
		Provider: "Anthropic", // Mixed case from some provider implementations.
		Model:    "claude-opus-4-7",
		Kind:     "completion",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exp.Shutdown(ctx)

	spans := fc.snapshot()
	if len(spans) == 0 {
		t.Fatal("no spans received")
	}
	attrs := attrMap(spans[0].Attributes)
	if attrs["gen_ai.system"] != "anthropic" {
		t.Errorf("gen_ai.system = %q, want anthropic (lowercased)", attrs["gen_ai.system"])
	}
}

func TestLaminarExporter_SpanNameFallback(t *testing.T) {
	// When AgentID is empty, span name should be "llm.<kind>".
	exp, fc := newLaminarExporterForTest(t)

	exp.Record(LLMTrace{
		Provider: "ollama",
		Model:    "qwen2.5:7b",
		Kind:     "completion",
		// AgentID intentionally absent.
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exp.Shutdown(ctx)

	spans := fc.snapshot()
	if len(spans) == 0 {
		t.Fatal("no spans received")
	}
	if spans[0].Name != "llm.completion" {
		t.Errorf("span name = %q, want llm.completion", spans[0].Name)
	}
}

func TestLaminarExporter_PHICaseIDHashed(t *testing.T) {
	exp, fc := newLaminarExporterForTest(t)

	exp.Record(LLMTrace{
		Provider: "anthropic",
		Model:    "m",
		CaseID:   "MRN-12345",
		Kind:     "completion",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exp.Shutdown(ctx)

	spans := fc.snapshot()
	if len(spans) == 0 {
		t.Fatal("no spans received")
	}
	attrs := attrMap(spans[0].Attributes)

	caseAttr := attrs["bodh.case_id"]
	if caseAttr == "MRN-12345" {
		t.Error("raw MRN leaked into Laminar span — PHI violation")
	}
	if caseAttr == "" {
		t.Error("bodh.case_id missing — correlation broken")
	}
	if strings.Contains(caseAttr, "MRN") {
		t.Errorf("bodh.case_id contains MRN substring: %q", caseAttr)
	}
}

func TestLaminarExporter_NoCachedTokensWhenZero(t *testing.T) {
	exp, fc := newLaminarExporterForTest(t)

	exp.Record(LLMTrace{
		Provider:     "openai",
		Model:        "gpt-4o",
		CachedTokens: 0, // no cache hit
		Kind:         "completion",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exp.Shutdown(ctx)

	spans := fc.snapshot()
	if len(spans) == 0 {
		t.Fatal("no spans received")
	}
	// cache_read_input_tokens should NOT be present when CachedTokens == 0.
	for _, a := range spans[0].Attributes {
		if a.Key == "gen_ai.usage.cache_read_input_tokens" {
			t.Errorf("cache_read_input_tokens should be absent when 0, got %v", a.Value)
		}
	}
}

func TestLaminarExporter_NilSafe(t *testing.T) {
	var e *LaminarExporter
	// Should not panic.
	e.Record(LLMTrace{Provider: "p", Model: "m"})
	if err := e.Shutdown(context.Background()); err != nil {
		t.Errorf("nil Shutdown returned err: %v", err)
	}
}

func TestLaminarExporter_ErrorRecorded(t *testing.T) {
	exp, fc := newLaminarExporterForTest(t)

	exp.Record(LLMTrace{
		Provider: "anthropic",
		Model:    "m",
		Error:    "context deadline exceeded",
		Kind:     "completion",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exp.Shutdown(ctx)

	spans := fc.snapshot()
	if len(spans) == 0 {
		t.Fatal("no spans received")
	}
	// A recorded error should appear in the span's events.
	if len(spans[0].Events) == 0 {
		t.Error("want span event for recorded error, got none")
	}
}

// intAttrMap extracts integer attribute values from a span for assertions.
func intAttrMap(kvs []*commonpb.KeyValue) map[string]int64 {
	out := map[string]int64{}
	for _, kv := range kvs {
		if v := kv.Value.GetIntValue(); v != 0 {
			out[kv.Key] = v
		}
	}
	return out
}
