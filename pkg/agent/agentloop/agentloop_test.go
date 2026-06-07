package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// ---------------------------------------------------------------------------
// Mock LLM client
// ---------------------------------------------------------------------------

type mockClient struct {
	mu        sync.Mutex
	responses []*llm.Response
	errors    []error
	calls     int
}

func (m *mockClient) Provider() string { return "mock" }
func (m *mockClient) Model() string    { return "mock-model" }

func (m *mockClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	m.mu.Lock()
	i := m.calls
	m.calls++
	m.mu.Unlock()

	if i < len(m.errors) && m.errors[i] != nil {
		return nil, m.errors[i]
	}
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return &llm.Response{Content: "fallback"}, nil
}

// ---------------------------------------------------------------------------
// RunAgent tests
// ---------------------------------------------------------------------------

func TestRunAgent_FinalTextNoToolCalls(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{
			{Content: "hello"},
		},
	}

	got, err := RunAgent(context.Background(), client, "hi", 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
	if client.calls != 1 {
		t.Fatalf("expected 1 call, got %d", client.calls)
	}
}

func TestRunAgent_SingleToolCallLoop(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{
					{ID: "t1", Name: "getDateTime", Arguments: nil},
				},
			},
			{Content: "The current time is 2025-01-01T00:00:00Z"},
		},
	}

	got, err := RunAgent(context.Background(), client, "what time is it?", 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "The current time is 2025-01-01T00:00:00Z" {
		t.Fatalf("got %q, want final text response", got)
	}
	if client.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", client.calls)
	}
}

func TestRunAgent_MaxIterationsExceeded(t *testing.T) {
	// Always return a tool call — never a final text response.
	client := &mockClient{
		responses: []*llm.Response{
			{ToolCalls: []llm.ToolCall{{ID: "t1", Name: "getDateTime"}}},
			{ToolCalls: []llm.ToolCall{{ID: "t2", Name: "getDateTime"}}},
			{ToolCalls: []llm.ToolCall{{ID: "t3", Name: "getDateTime"}}},
		},
	}

	_, err := RunAgent(context.Background(), client, "loop forever", 3, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("error %q does not mention max iterations", err.Error())
	}
}

func TestRunAgent_LLMErrorPropagation(t *testing.T) {
	injected := errors.New("provider unavailable")
	client := &mockClient{
		errors: []error{injected},
	}

	_, err := RunAgent(context.Background(), client, "hi", 5, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, injected) {
		t.Fatalf("error %q does not wrap injected error", err.Error())
	}
	if !strings.Contains(err.Error(), "agentloop") {
		t.Fatalf("error %q missing agentloop prefix", err.Error())
	}
}

func TestRunAgent_CallbacksFire(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{
					{ID: "t1", Name: "getDateTime", Arguments: nil},
				},
			},
			{Content: "done"},
		},
	}

	var order []string
	cbs := &AgentCallbacks{
		OnStart: func(msg string) {
			order = append(order, "start:"+msg)
		},
		OnToolCall: func(name string, _ json.RawMessage) {
			order = append(order, "toolcall:"+name)
		},
		OnToolResult: func(name string, _ string) {
			order = append(order, "toolresult:"+name)
		},
		OnFinish: func(resp string) {
			order = append(order, "finish:"+resp)
		},
	}

	got, err := RunAgent(context.Background(), client, "what time?", 5, cbs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "done" {
		t.Fatalf("got %q, want %q", got, "done")
	}

	expected := []string{
		"start:what time?",
		"toolcall:getDateTime",
		"toolresult:getDateTime",
		"finish:done",
	}
	if len(order) != len(expected) {
		t.Fatalf("callback order = %v, want %v", order, expected)
	}
	for i, want := range expected {
		if order[i] != want {
			t.Fatalf("callback[%d] = %q, want %q", i, order[i], want)
		}
	}
}

func TestRunAgent_NilCallbacksSafe(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{
					{ID: "t1", Name: "getDateTime"},
				},
			},
			{Content: "done"},
		},
	}

	// Must not panic with nil cbs.
	got, err := RunAgent(context.Background(), client, "hi", 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "done" {
		t.Fatalf("got %q, want %q", got, "done")
	}
}

func TestRunAgent_DefaultMaxIter(t *testing.T) {
	// Supply more than MaxIterations (10) responses, all tool calls.
	responses := make([]*llm.Response, MaxIterations+5)
	for i := range responses {
		responses[i] = &llm.Response{
			ToolCalls: []llm.ToolCall{{ID: "t", Name: "getDateTime"}},
		}
	}
	client := &mockClient{responses: responses}

	// maxIter=0 should default to MaxIterations (10).
	_, err := RunAgent(context.Background(), client, "loop", 0, nil)
	if err == nil {
		t.Fatal("expected max iterations error, got nil")
	}
	if !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("error %q does not mention max iterations", err.Error())
	}
	// The loop should have made exactly MaxIterations calls.
	if client.calls != MaxIterations {
		t.Fatalf("expected %d calls, got %d", MaxIterations, client.calls)
	}
}

// ---------------------------------------------------------------------------
// ExecuteTool tests
// ---------------------------------------------------------------------------

func TestExecuteTool_KnownTool(t *testing.T) {
	result, err := ExecuteTool("getDateTime", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result for getDateTime")
	}
}

func TestExecuteTool_UnknownTool(t *testing.T) {
	_, err := ExecuteTool("bogus", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("error %q does not mention unknown tool", err.Error())
	}
}

func TestExecuteTool_InvalidJSONArgs(t *testing.T) {
	_, err := ExecuteTool("getDateTime", []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON args, got nil")
	}
	if !strings.Contains(err.Error(), "decode args") {
		t.Fatalf("error %q does not mention decode args", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tools() tests
// ---------------------------------------------------------------------------

func TestTools_ReturnsList(t *testing.T) {
	tools := Tools()
	if len(tools) < 2 {
		t.Fatalf("expected at least 2 tools, got %d", len(tools))
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"getDateTime", "getWeather"} {
		if !names[want] {
			t.Fatalf("tool %q not found in Tools() result", want)
		}
	}
}
