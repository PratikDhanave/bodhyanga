package observability_test

import (
	"context"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
)

// ── stubs ─────────────────────────────────────────────────────────────────────

// stubInner is a minimal llm.Client for testing.
type stubInner struct {
	provider string
	model    string
	resp     *llm.Response
	err      error
}

func (s *stubInner) Provider() string { return s.provider }
func (s *stubInner) Model() string    { return s.model }
func (s *stubInner) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return s.resp, s.err
}

// recordingRecorder captures every LLMTrace for assertions.
type recordingRecorder struct {
	traces []observability.LLMTrace
}

func (r *recordingRecorder) RecordLLMCall(t observability.LLMTrace) {
	r.traces = append(r.traces, t)
}

// ── TracingClient tests ────────────────────────────────────────────────────────

func TestTracingClient_DelegatesProviderAndModel(t *testing.T) {
	inner := &stubInner{provider: "anthropic", model: "claude-haiku-4-5"}
	rec := &recordingRecorder{}
	c := observability.NewTracingClient(inner, rec, "test-agent")

	if c.Provider() != "anthropic" {
		t.Errorf("Provider = %q, want anthropic", c.Provider())
	}
	if c.Model() != "claude-haiku-4-5" {
		t.Errorf("Model = %q, want claude-haiku-4-5", c.Model())
	}
}

func TestTracingClient_RecordsOnSuccess(t *testing.T) {
	inner := &stubInner{
		provider: "openai",
		model:    "gpt-4o",
		resp: &llm.Response{
			Content:      "hello",
			InputTokens:  10,
			OutputTokens: 5,
			CachedTokens: 2,
			StopReason:   "stop",
			ToolCalls:    []llm.ToolCall{{Name: "fn"}},
		},
	}
	rec := &recordingRecorder{}
	c := observability.NewTracingClient(inner, rec, "my-agent")

	resp, err := c.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("response content = %q, want hello", resp.Content)
	}
	if len(rec.traces) != 1 {
		t.Fatalf("want 1 trace, got %d", len(rec.traces))
	}
	tr := rec.traces[0]
	if tr.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", tr.Provider)
	}
	if tr.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", tr.Model)
	}
	if tr.AgentID != "my-agent" {
		t.Errorf("AgentID = %q, want my-agent", tr.AgentID)
	}
	if tr.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", tr.InputTokens)
	}
	if tr.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", tr.OutputTokens)
	}
	if tr.CachedTokens != 2 {
		t.Errorf("CachedTokens = %d, want 2", tr.CachedTokens)
	}
	if tr.StopReason != "stop" {
		t.Errorf("StopReason = %q, want stop", tr.StopReason)
	}
	if tr.ToolCalls != 1 {
		t.Errorf("ToolCalls = %d, want 1", tr.ToolCalls)
	}
	if tr.Error != "" {
		t.Errorf("Error should be empty on success, got %q", tr.Error)
	}
	if tr.Kind != "completion" {
		t.Errorf("Kind = %q, want completion", tr.Kind)
	}
	if tr.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >= 0", tr.DurationMs)
	}
}

func TestTracingClient_RecordsOnError(t *testing.T) {
	boom := errors.New("network timeout")
	inner := &stubInner{provider: "ollama", model: "qwen2.5:7b", err: boom}
	rec := &recordingRecorder{}
	c := observability.NewTracingClient(inner, rec, "agent")

	_, err := c.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("want boom error, got %v", err)
	}
	if len(rec.traces) != 1 {
		t.Fatalf("want 1 trace even on error, got %d", len(rec.traces))
	}
	if rec.traces[0].Error == "" {
		t.Error("trace.Error should be non-empty on error")
	}
	// Tokens should be zero (no response).
	if rec.traces[0].InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0 on error", rec.traces[0].InputTokens)
	}
}

func TestTracingClient_WithCaseIDAndTraceID(t *testing.T) {
	inner := &stubInner{provider: "anthropic", model: "m", resp: &llm.Response{}}
	rec := &recordingRecorder{}
	c := observability.NewTracingClient(inner, rec, "agent",
		observability.WithCaseID("MRN-12345"),
		observability.WithTraceID("trace-abc"),
	)

	_, _ = c.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if len(rec.traces) != 1 {
		t.Fatalf("want 1 trace, got %d", len(rec.traces))
	}
	tr := rec.traces[0]
	if tr.CaseID != "MRN-12345" {
		t.Errorf("CaseID = %q, want MRN-12345", tr.CaseID)
	}
	if tr.TraceID != "trace-abc" {
		t.Errorf("TraceID = %q, want trace-abc", tr.TraceID)
	}
}

func TestTracingClient_NilResponseSafe(t *testing.T) {
	// When inner returns (nil, err), TracingClient must not panic.
	inner := &stubInner{provider: "p", model: "m", err: errors.New("fail")}
	rec := &recordingRecorder{}
	c := observability.NewTracingClient(inner, rec, "agent")

	_, err := c.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("want error")
	}
	if len(rec.traces) != 1 {
		t.Fatalf("want 1 trace, got %d", len(rec.traces))
	}
}
