package observability

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- Laminar stub (0%) ----

func TestLaminarExporter_Stub(t *testing.T) {
	exp, err := NewLaminarExporter(context.Background())
	if err == nil {
		t.Error("expected error from Laminar stub")
	}
	if exp != nil {
		t.Error("expected nil exporter")
	}

	// Exercise no-op methods.
	var stub LaminarExporter
	stub.Record(LLMTrace{})
	if err := stub.Shutdown(context.Background()); err != nil {
		t.Errorf("unexpected error from Shutdown: %v", err)
	}
}

// ---- OTLP stub (0%) ----

func TestOTLPLLMExporter_Stub(t *testing.T) {
	exp, err := NewOTLPLLMExporter(context.Background(), "http://localhost:4318")
	if err == nil {
		t.Error("expected error from OTLP stub")
	}
	if exp != nil {
		t.Error("expected nil exporter")
	}

	// Exercise no-op methods.
	var stub OTLPLLMExporter
	stub.Record(LLMTrace{})
	if err := stub.Shutdown(context.Background()); err != nil {
		t.Errorf("unexpected error from Shutdown: %v", err)
	}
}

// ---- NewStdLogger (0%) ----

func TestNewStdLogger(t *testing.T) {
	logger := NewStdLogger()
	if logger == nil {
		t.Fatal("NewStdLogger returned nil")
	}
	// Should not panic.
	logger.Printf("test %d", 42)
}

// ---- SystemClock.Now (0%) ----

func TestSystemClock_Now(t *testing.T) {
	c := SystemClock{}
	now := c.Now()
	if now.IsZero() {
		t.Error("Now() should return non-zero time")
	}
}

// ---- TraceHandler stub (0%) ----

func TestTraceHandler_Stub(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := TraceHandler(inner, "test-op")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

// ---- InitGlobalTracer stub (0%) ----

func TestInitGlobalTracer_Stub(t *testing.T) {
	shutdown, err := InitGlobalTracer(context.Background(), "bodh")
	if err == nil {
		t.Error("expected error from stub InitGlobalTracer")
	}
	if shutdown == nil {
		t.Fatal("shutdown func should not be nil")
	}
	// shutdown should be a no-op.
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown error: %v", err)
	}
}

// ---- PrometheusHandler (0%) ----

func TestPrometheusHandler(t *testing.T) {
	h := PrometheusHandler()
	if h == nil {
		t.Fatal("PrometheusHandler returned nil")
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

// ---- RecordLLMMetrics with zero tokens ----

func TestRecordLLMMetrics_ZeroTokens(t *testing.T) {
	// Should not panic for zero values.
	RecordLLMMetrics(LLMTrace{
		Provider:     "test",
		Model:        "test-model",
		InputTokens:  0,
		OutputTokens: 0,
		CachedTokens: 0,
		DurationMs:   100,
	})
}

func TestRecordLLMMetrics_EmptyKindAndAgentID(t *testing.T) {
	// Should default to "completion" kind and "unknown" agent.
	RecordLLMMetrics(LLMTrace{
		Provider: "test",
		Model:    "test-model",
	})
}

func TestRecordLLMMetrics_WithCachedTokens(t *testing.T) {
	RecordLLMMetrics(LLMTrace{
		Provider:     "test",
		Model:        "test-model",
		InputTokens:  100,
		OutputTokens: 50,
		CachedTokens: 30,
		Kind:         "embedding",
		AgentID:      "test-agent",
		DurationMs:   200,
	})
}

// ---- RecordLLMCall with hook ----

func TestRecordLLMCall_WithOTelHook(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerTo(&buf)

	var hookTrace LLMTrace
	hookCalled := false
	SetLLMOTelHook(func(trace LLMTrace) {
		hookTrace = trace
		hookCalled = true
	})
	defer SetLLMOTelHook(nil)

	t.Setenv("BODH_OTEL_ENABLED", "1")

	logger.RecordLLMCall(LLMTrace{
		Provider:    "anthropic",
		Model:       "claude-sonnet",
		InputTokens: 100,
	})

	if !hookCalled {
		t.Error("expected OTel hook to be called")
	}
	if hookTrace.Kind != "completion" {
		t.Errorf("expected normalized kind 'completion', got %q", hookTrace.Kind)
	}
}

func TestRecordLLMCall_WithoutOTelEnabled(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerTo(&buf)

	hookCalled := false
	SetLLMOTelHook(func(_ LLMTrace) {
		hookCalled = true
	})
	defer SetLLMOTelHook(nil)

	t.Setenv("BODH_OTEL_ENABLED", "0")

	logger.RecordLLMCall(LLMTrace{
		Provider: "test",
		Model:    "test",
	})

	if hookCalled {
		t.Error("OTel hook should NOT be called when BODH_OTEL_ENABLED != 1")
	}
}

// ---- PromMiddleware /metrics skip ----

func TestPromMiddleware_SkipMetrics(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := PromMiddleware(handler)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	wrapped.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

// ---- statusRecorder code() ----

func TestStatusRecorder_DefaultCode(t *testing.T) {
	rr := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rr}
	// No WriteHeader called — code() should return 200.
	if sr.code() != http.StatusOK {
		t.Errorf("default code = %d, want 200", sr.code())
	}
}

func TestStatusRecorder_DoubleWriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rr}
	sr.WriteHeader(http.StatusCreated)
	sr.WriteHeader(http.StatusNotFound) // should be no-op
	if sr.code() != http.StatusCreated {
		t.Errorf("code = %d, want 201", sr.code())
	}
}

// ---- normalizePath edge cases ----

func TestNormalizePath_EmptyAfterTrim(t *testing.T) {
	got := normalizePath("/")
	if got != "/" {
		t.Errorf("normalizePath(/) = %q, want /", got)
	}
}

func TestNormalizePath_Empty(t *testing.T) {
	got := normalizePath("")
	if got != "/" {
		t.Errorf("normalizePath('') = %q, want /", got)
	}
}

func TestNormalizePath_HexSegment(t *testing.T) {
	got := normalizePath("/events/abcdef0123456789")
	if got != "/events/{id}" {
		t.Errorf("normalizePath hex = %q, want /events/{id}", got)
	}
}

func TestNormalizePath_NumericSegment(t *testing.T) {
	got := normalizePath("/page/42")
	if got != "/page/{id}" {
		t.Errorf("normalizePath numeric = %q, want /page/{id}", got)
	}
}
