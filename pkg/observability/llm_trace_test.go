package observability

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRecordLLMCall_WritesJSONLine(t *testing.T) {
	buf := &bytes.Buffer{}
	l := NewLoggerTo(buf)

	trace := LLMTrace{
		StartedAt:    time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		DurationMs:   123,
		Provider:     "anthropic",
		Model:        "claude-opus-4",
		AgentID:      "triage",
		CaseID:       "case-1",
		TraceID:      "trace-1",
		InputTokens:  100,
		OutputTokens: 50,
		CachedTokens: 10,
		StopReason:   "end_turn",
		ToolCalls:    2,
		Kind:         "completion",
	}
	l.RecordLLMCall(trace)

	out := buf.String()
	if !strings.Contains(out, "[llm-trace]") {
		t.Fatalf("output missing [llm-trace] tag: %q", out)
	}
	// Extract JSON payload after the "[llm-trace] " marker.
	idx := strings.Index(out, "[llm-trace] ")
	if idx < 0 {
		t.Fatalf("could not find marker in: %q", out)
	}
	jsonPart := strings.TrimSpace(out[idx+len("[llm-trace] "):])
	// jsonPart may include trailing newline already trimmed; ensure it's JSON.
	var got LLMTrace
	if err := json.Unmarshal([]byte(jsonPart), &got); err != nil {
		t.Fatalf("payload was not valid JSON: %v\npayload: %q", err, jsonPart)
	}
	if got.Provider != "anthropic" || got.Model != "claude-opus-4" {
		t.Errorf("provider/model not round-tripped: %+v", got)
	}
	if got.InputTokens != 100 || got.OutputTokens != 50 || got.CachedTokens != 10 {
		t.Errorf("token counts not round-tripped: %+v", got)
	}
	if got.AgentID != "triage" || got.CaseID != "case-1" || got.TraceID != "trace-1" {
		t.Errorf("ids not round-tripped: %+v", got)
	}
	if got.Kind != "completion" {
		t.Errorf("kind not round-tripped: got %q", got.Kind)
	}
}

func TestRecordLLMCall_NormalisesKind(t *testing.T) {
	buf := &bytes.Buffer{}
	l := NewLoggerTo(buf)

	l.RecordLLMCall(LLMTrace{Provider: "p", Model: "m"}) // Kind intentionally empty

	out := buf.String()
	idx := strings.Index(out, "[llm-trace] ")
	if idx < 0 {
		t.Fatalf("no marker in output: %q", out)
	}
	payload := strings.TrimSpace(out[idx+len("[llm-trace] "):])
	var got LLMTrace
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Kind != "completion" {
		t.Errorf("expected normalised Kind=\"completion\", got %q", got.Kind)
	}
}

func TestSetLLMOTelHook_FiresOnlyWhenEnvSet(t *testing.T) {
	t.Cleanup(func() {
		SetLLMOTelHook(nil)
		_ = os.Unsetenv("BODH_OTEL_ENABLED")
	})

	var calls []LLMTrace
	SetLLMOTelHook(func(tr LLMTrace) { calls = append(calls, tr) })

	buf := &bytes.Buffer{}
	l := NewLoggerTo(buf)

	// First call: env NOT set -> hook should NOT fire.
	_ = os.Unsetenv("BODH_OTEL_ENABLED")
	l.RecordLLMCall(LLMTrace{Provider: "p", Model: "m", AgentID: "no-otel"})
	if len(calls) != 0 {
		t.Fatalf("hook fired without env set: %d calls", len(calls))
	}

	// Second call: env set -> hook should fire exactly once.
	if err := os.Setenv("BODH_OTEL_ENABLED", "1"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	l.RecordLLMCall(LLMTrace{Provider: "p", Model: "m", AgentID: "with-otel"})
	if len(calls) != 1 {
		t.Fatalf("expected exactly one hook call after enabling env, got %d", len(calls))
	}
	if calls[0].AgentID != "with-otel" {
		t.Errorf("hook received wrong trace: %+v", calls[0])
	}
}
