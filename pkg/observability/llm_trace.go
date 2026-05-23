package observability

import (
	"encoding/json"
	"os"
	"time"
)

// LLMTrace is a structured record of one LLM (or embedding) call. Agents
// build one of these around each Complete()/Embed() call and hand it to a
// Logger via RecordLLMCall.
//
// Fields are intentionally flat for easy ingestion into log aggregators and
// future OTel span attribute mapping.
type LLMTrace struct {
	// When the call started (RFC3339).
	StartedAt time.Time `json:"started_at"`

	// DurationMs is the wall-clock latency of the call.
	DurationMs int64 `json:"duration_ms"`

	// Provider / Model identify the LLM. AgentID identifies the caller.
	Provider string `json:"provider"`
	Model    string `json:"model"`
	AgentID  string `json:"agent_id,omitempty"`

	// CaseID and TraceID propagate from message metadata so log queries can
	// reconstruct a full case.
	CaseID  string `json:"case_id,omitempty"`
	TraceID string `json:"trace_id,omitempty"`

	// Token accounting.
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CachedTokens int `json:"cached_tokens"`

	// StopReason is the provider's stop reason ("end_turn", "tool_use", ...).
	StopReason string `json:"stop_reason,omitempty"`

	// ToolCalls counts how many tool invocations the model produced.
	ToolCalls int `json:"tool_calls,omitempty"`

	// Error, if non-empty, indicates the call failed.
	Error string `json:"error,omitempty"`

	// Kind distinguishes "completion" from "embedding" calls.
	Kind string `json:"kind"`
}

// RecordLLMCall serialises the trace and emits it via the logger. With a
// StdLogger this becomes one JSON line on stdout prefixed with "[llm-trace]".
//
// When the BODH_OTEL_ENABLED=1 env var is set (and an OTel collector hook
// has been installed via SetLLMOTelHook), the same trace is forwarded to
// the hook for OTLP export. The default build links no OTel SDK; tracing is
// opt-in to keep the dep graph minimal.
func (l *StdLogger) RecordLLMCall(t LLMTrace) {
	t.normalise()
	b, _ := json.Marshal(t)
	l.Printf("[llm-trace] %s", string(b))
	if otelHook != nil && os.Getenv("BODH_OTEL_ENABLED") == "1" {
		otelHook(t)
	}
}

// normalise fills derived fields. Idempotent.
func (t *LLMTrace) normalise() {
	if t.Kind == "" {
		t.Kind = "completion"
	}
}

// otelHook receives LLMTrace records when OTel export is enabled. The default
// is nil (no-op). Wire up real OTLP export in cmd/* main if desired.
var otelHook func(LLMTrace)

// SetLLMOTelHook installs a function called for every LLMTrace when
// BODH_OTEL_ENABLED=1 is set in the environment. Passing nil clears it.
//
// Typical wiring (in cmd/*/main.go):
//
//	otel := observability.NewOTLPLLMExporter("http://localhost:4318")
//	observability.SetLLMOTelHook(otel.Record)
//	defer otel.Shutdown(ctx)
//
// The exporter type lives in observability_otel.go and is build-tag gated so
// the default Bodh binary doesn't pull in the OTel SDK.
func SetLLMOTelHook(fn func(LLMTrace)) { otelHook = fn }
