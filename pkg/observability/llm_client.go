// llm_client.go — TracingClient: an llm.Client wrapper that records an
// LLMTrace for every Complete call.
//
// Usage:
//
//	logger := observability.NewStdLogger(os.Stdout)
//	client := observability.NewTracingClient(
//	    llm.NewAnthropic(apiKey, model),
//	    logger,
//	    "my-agent",   // agentID — shown in Laminar timeline
//	)
//	// client satisfies llm.Client; swap in transparently.
//
// Wiring with Laminar:
//
//	exp, _ := observability.NewLaminarExporter(ctx)  // -tags otel
//	observability.SetLLMOTelHook(exp.Record)
//	logger := observability.NewStdLogger(os.Stdout)
//	client := observability.NewTracingClient(llm.NewOllama(host, model), logger, "agent")
//
// Every Complete call is timed; on return an LLMTrace is built from the
// Response (tokens, stop reason, tool calls) and handed to the Recorder.
// The Recorder can be any *observability.StdLogger (which logs + fires the
// OTel hook) or a test double. Errors are recorded in LLMTrace.Error but
// still propagated to the caller unchanged.

package observability

import (
	"context"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

// LLMRecorder is the minimal sink TracingClient uses to report each LLMTrace.
// *StdLogger satisfies this interface; tests can substitute a recording double.
type LLMRecorder interface {
	RecordLLMCall(LLMTrace)
}

// TracingClient wraps an llm.Client and records an LLMTrace for each Complete
// call. It is transparent: Provider(), Model(), and Complete() delegate to the
// inner client; only timing and token metadata are added.
type TracingClient struct {
	inner    llm.Client
	recorder LLMRecorder
	agentID  string
	caseID   string // optional; set via WithCaseID
	traceID  string // optional; set via WithTraceID
}

// TracingOption configures a TracingClient after construction.
type TracingOption func(*TracingClient)

// WithCaseID attaches a case identifier to every LLMTrace. The value is
// hashed (sha256) before reaching OTLP backends so raw MRNs cannot leak.
func WithCaseID(id string) TracingOption {
	return func(c *TracingClient) { c.caseID = id }
}

// WithTraceID propagates a distributed trace correlation ID into LLMTrace
// records. Use this when the caller maintains its own trace/session IDs.
func WithTraceID(id string) TracingOption {
	return func(c *TracingClient) { c.traceID = id }
}

// NewTracingClient wraps inner with automatic LLMTrace recording.
//
// Parameters:
//   - inner    — the real LLM client (Anthropic, Ollama, etc.)
//   - recorder — where to send traces; typically *observability.StdLogger
//   - agentID  — human-readable label for this client in Laminar / logs
//   - opts     — optional: WithCaseID, WithTraceID
//
// The returned client satisfies llm.Client and can replace inner anywhere.
func NewTracingClient(inner llm.Client, recorder LLMRecorder, agentID string, opts ...TracingOption) *TracingClient {
	c := &TracingClient{
		inner:    inner,
		recorder: recorder,
		agentID:  agentID,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Provider delegates to the inner client.
func (c *TracingClient) Provider() string { return c.inner.Provider() }

// Model delegates to the inner client.
func (c *TracingClient) Model() string { return c.inner.Model() }

// Complete delegates to the inner client, records an LLMTrace, and returns
// the response unchanged. Errors are stored in LLMTrace.Error and still
// returned to the caller.
func (c *TracingClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	start := time.Now()
	resp, err := c.inner.Complete(ctx, req)
	durationMs := time.Since(start).Milliseconds()

	t := LLMTrace{
		StartedAt:  start,
		DurationMs: durationMs,
		Provider:   c.inner.Provider(),
		Model:      c.inner.Model(),
		AgentID:    c.agentID,
		CaseID:     c.caseID,
		TraceID:    c.traceID,
		Kind:       "completion",
	}
	if resp != nil {
		t.InputTokens = resp.InputTokens
		t.OutputTokens = resp.OutputTokens
		t.CachedTokens = resp.CachedTokens
		t.StopReason = resp.StopReason
		t.ToolCalls = len(resp.ToolCalls)
	}
	if err != nil {
		t.Error = err.Error()
	}

	c.recorder.RecordLLMCall(t)
	return resp, err
}
