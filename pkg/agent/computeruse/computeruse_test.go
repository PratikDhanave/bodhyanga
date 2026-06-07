package computeruse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/tools"
)

// mockClient is a deterministic llm.Client that replays a sequence of
// responses (or errors). After all scripted responses are exhausted it
// returns a fallback text response.
type mockClient struct {
	responses []*llm.Response
	errors    []error
	idx       int
}

func (m *mockClient) Provider() string { return "mock" }
func (m *mockClient) Model() string    { return "mock-model" }

func (m *mockClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	i := m.idx
	m.idx++
	if i < len(m.errors) && m.errors[i] != nil {
		return nil, m.errors[i]
	}
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return &llm.Response{Content: "fallback"}, nil
}

// ---------------------------------------------------------------------------
// 1. Direct text response (no tool calls)
// ---------------------------------------------------------------------------

func TestRunAgent_DirectTextResponse(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{
			{Content: "Hello, world!", StopReason: "end_turn"},
		},
	}

	var startCalled, finishCalled bool
	var startMsg, finishMsg string
	cbs := &AgentCallbacks{
		OnStart:  func(msg string) { startCalled = true; startMsg = msg },
		OnFinish: func(resp string) { finishCalled = true; finishMsg = resp },
	}

	got, err := RunAgent(context.Background(), client, "greet me", 5, cbs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Hello, world!" {
		t.Errorf("want %q, got %q", "Hello, world!", got)
	}
	if !startCalled {
		t.Error("OnStart was not called")
	}
	if startMsg != "greet me" {
		t.Errorf("OnStart: want %q, got %q", "greet me", startMsg)
	}
	if !finishCalled {
		t.Error("OnFinish was not called")
	}
	if finishMsg != "Hello, world!" {
		t.Errorf("OnFinish: want %q, got %q", "Hello, world!", finishMsg)
	}
}

// ---------------------------------------------------------------------------
// 2. Tool call then text
// ---------------------------------------------------------------------------

func TestRunAgent_ToolCallThenText(t *testing.T) {
	cmdArgs, _ := json.Marshal(map[string]string{"command": "pwd"})
	client := &mockClient{
		responses: []*llm.Response{
			{
				Content: "Let me check.",
				ToolCalls: []llm.ToolCall{
					{ID: "tc_1", Name: "run_command", Arguments: cmdArgs},
				},
			},
			{Content: "The current directory is /home/user.", StopReason: "end_turn"},
		},
	}

	got, err := RunAgent(context.Background(), client, "where am I?", 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "current directory") {
		t.Errorf("expected final answer to contain 'current directory', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 3. Unknown tool handled gracefully
// ---------------------------------------------------------------------------

func TestRunAgent_UnknownToolHandledGracefully(t *testing.T) {
	bogusArgs, _ := json.Marshal(map[string]string{"foo": "bar"})
	client := &mockClient{
		responses: []*llm.Response{
			{
				Content: "Trying bogus tool.",
				ToolCalls: []llm.ToolCall{
					{ID: "tc_bogus", Name: "bogus_tool", Arguments: bogusArgs},
				},
			},
			{Content: "I could not find that tool.", StopReason: "end_turn"},
		},
	}

	got, err := RunAgent(context.Background(), client, "do something", 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "I could not find that tool." {
		t.Errorf("want %q, got %q", "I could not find that tool.", got)
	}
}

// ---------------------------------------------------------------------------
// 4. Max iterations reached
// ---------------------------------------------------------------------------

func TestRunAgent_MaxIterations(t *testing.T) {
	cmdArgs, _ := json.Marshal(map[string]string{"command": "pwd"})
	// Every response contains a tool call, so the loop never ends naturally.
	alwaysToolCall := &llm.Response{
		Content: "still working",
		ToolCalls: []llm.ToolCall{
			{ID: "tc_loop", Name: "run_command", Arguments: cmdArgs},
		},
	}
	client := &mockClient{
		responses: []*llm.Response{
			alwaysToolCall, alwaysToolCall, alwaysToolCall,
			alwaysToolCall, alwaysToolCall,
		},
	}

	maxIter := 3
	_, err := RunAgent(context.Background(), client, "loop forever", maxIter, nil)
	if err == nil {
		t.Fatal("expected an error for max iterations, got nil")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("max iterations (%d)", maxIter)) {
		t.Errorf("error should mention max iterations, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 5. LLM error
// ---------------------------------------------------------------------------

func TestRunAgent_LLMError(t *testing.T) {
	llmErr := errors.New("provider unavailable")
	client := &mockClient{
		errors: []error{llmErr},
	}

	_, err := RunAgent(context.Background(), client, "fail", 5, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "computeruse: iteration 1") {
		t.Errorf("error should reference iteration 1, got: %v", err)
	}
	if !errors.Is(err, llmErr) {
		t.Errorf("error should wrap original LLM error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 6. Nil callbacks (no panic)
// ---------------------------------------------------------------------------

func TestRunAgent_NilCallbacks(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{
			{Content: "ok", StopReason: "end_turn"},
		},
	}

	got, err := RunAgent(context.Background(), client, "hello", 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("want %q, got %q", "ok", got)
	}
}

// ---------------------------------------------------------------------------
// 7. All callbacks fire (with counters)
// ---------------------------------------------------------------------------

func TestRunAgent_AllCallbacksFire(t *testing.T) {
	cmdArgs, _ := json.Marshal(map[string]string{"command": "pwd"})
	client := &mockClient{
		responses: []*llm.Response{
			{
				Content: "calling tool",
				ToolCalls: []llm.ToolCall{
					{ID: "tc_a", Name: "run_command", Arguments: cmdArgs},
				},
			},
			{Content: "done", StopReason: "end_turn"},
		},
	}

	var startCount, actionCount, resultCount, finishCount int32
	cbs := &AgentCallbacks{
		OnStart:          func(_ string) { atomic.AddInt32(&startCount, 1) },
		OnComputerAction: func(_ string, _ json.RawMessage) { atomic.AddInt32(&actionCount, 1) },
		OnToolResult:     func(_ string, _ json.RawMessage) { atomic.AddInt32(&resultCount, 1) },
		OnFinish:         func(_ string) { atomic.AddInt32(&finishCount, 1) },
	}

	_, err := RunAgent(context.Background(), client, "test callbacks", 5, cbs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if startCount != 1 {
		t.Errorf("OnStart: want 1 call, got %d", startCount)
	}
	if actionCount != 1 {
		t.Errorf("OnComputerAction: want 1 call, got %d", actionCount)
	}
	if resultCount != 1 {
		t.Errorf("OnToolResult: want 1 call, got %d", resultCount)
	}
	if finishCount != 1 {
		t.Errorf("OnFinish: want 1 call, got %d", finishCount)
	}
}

// ---------------------------------------------------------------------------
// 8. Content trimming
// ---------------------------------------------------------------------------

func TestRunAgent_ContentTrimming(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{
			{Content: "  padded  ", StopReason: "end_turn"},
		},
	}

	got, err := RunAgent(context.Background(), client, "trim test", 5, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "padded" {
		t.Errorf("want %q, got %q", "padded", got)
	}
}

// ---------------------------------------------------------------------------
// 9. invokeToolSafe — unknown tool returns JSON error
// ---------------------------------------------------------------------------

func TestInvokeToolSafe_UnknownTool(t *testing.T) {
	reg := tools.NewComputerRegistry()
	tc := llm.ToolCall{
		ID:        "tc_unknown",
		Name:      "nonexistent_tool",
		Arguments: json.RawMessage(`{}`),
	}

	result := invokeToolSafe(context.Background(), reg, tc)

	var parsed map[string]string
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v (raw: %s)", err, result)
	}
	errMsg, ok := parsed["error"]
	if !ok {
		t.Fatal("expected 'error' key in result")
	}
	if !strings.Contains(errMsg, "nonexistent_tool") {
		t.Errorf("error message should mention the tool name, got: %q", errMsg)
	}
}

// ---------------------------------------------------------------------------
// 10. Default maxIter when zero is passed
// ---------------------------------------------------------------------------

func TestRunAgent_DefaultMaxIter(t *testing.T) {
	// Pass maxIter=0 and have the client always return tool calls.
	// After MaxIterations (10) rounds the agent should return an error.
	cmdArgs, _ := json.Marshal(map[string]string{"command": "pwd"})
	alwaysToolCall := &llm.Response{
		Content: "looping",
		ToolCalls: []llm.ToolCall{
			{ID: "tc_inf", Name: "run_command", Arguments: cmdArgs},
		},
	}
	resps := make([]*llm.Response, MaxIterations+1)
	for i := range resps {
		resps[i] = alwaysToolCall
	}
	client := &mockClient{responses: resps}

	_, err := RunAgent(context.Background(), client, "default iter", 0, nil)
	if err == nil {
		t.Fatal("expected max-iterations error, got nil")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("max iterations (%d)", MaxIterations)) {
		t.Errorf("error should reference default MaxIterations (%d), got: %v", MaxIterations, err)
	}
}
