package toolcalling

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// --- stub client for toolcalling tests ---

type tcStubClient struct {
	response  string
	toolCalls []llm.ToolCall
	err       error
}

func (s *tcStubClient) Provider() string { return "stub" }
func (s *tcStubClient) Model() string    { return "stub-model" }
func (s *tcStubClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &llm.Response{
		Content:   s.response,
		ToolCalls: s.toolCalls,
	}, nil
}

// --- ExecuteTool: unknown tool ---

func TestExecuteTool_UnknownTool(t *testing.T) {
	_, err := ExecuteTool("nonexistent_tool", json.RawMessage("{}"))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// --- ExecuteTool: invalid JSON args ---

func TestExecuteTool_InvalidArgs(t *testing.T) {
	_, err := ExecuteTool("getDateTime", json.RawMessage("not json{{{"))
	if err == nil {
		t.Fatal("expected error for invalid JSON args")
	}
}

// --- ExecuteTool: null args ---

func TestExecuteTool_NullArgs(t *testing.T) {
	result, err := ExecuteTool("getDateTime", json.RawMessage("null"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result for getDateTime")
	}
}

// --- ExecuteTool: empty args ---

func TestExecuteTool_EmptyArgs(t *testing.T) {
	result, err := ExecuteTool("getDateTime", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

// --- RunAgent: LLM error ---

func TestRunAgent_LLMError(t *testing.T) {
	stub := &tcStubClient{err: errors.New("llm error")}
	_, err := RunAgent(context.Background(), stub, "hello", nil, AgentCallbacks{})
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

// --- RunAgent: tool call path with callbacks ---

func TestRunAgent_ToolCallWithCallbacks(t *testing.T) {
	stub := &tcStubClient{
		toolCalls: []llm.ToolCall{
			{Name: "getDateTime", Arguments: json.RawMessage(`{}`)},
		},
	}

	var toolNames []string
	var started, finished string
	cb := AgentCallbacks{
		OnStart:    func(msg string) { started = msg },
		OnToolCall: func(name string, args json.RawMessage) { toolNames = append(toolNames, name) },
		OnFinish:   func(resp string) { finished = resp },
	}

	result, err := RunAgent(context.Background(), stub, "what time is it", nil, cb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started != "what time is it" {
		t.Errorf("OnStart = %q, want %q", started, "what time is it")
	}
	if len(toolNames) != 1 || toolNames[0] != "getDateTime" {
		t.Errorf("OnToolCall names = %v, want [getDateTime]", toolNames)
	}
	if finished == "" {
		t.Error("OnFinish should be called with summary")
	}
	if result == "" {
		t.Error("expected non-empty result for tool call path")
	}
}

// --- RunAgent: text path with callbacks ---

func TestRunAgent_TextPathWithCallbacks(t *testing.T) {
	stub := &tcStubClient{response: "the answer"}

	var started, finished string
	cb := AgentCallbacks{
		OnStart:  func(msg string) { started = msg },
		OnFinish: func(resp string) { finished = resp },
	}

	result, err := RunAgent(context.Background(), stub, "question", nil, cb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "the answer" {
		t.Errorf("result = %q, want %q", result, "the answer")
	}
	if started != "question" {
		t.Errorf("started = %q, want %q", started, "question")
	}
	if finished != "the answer" {
		t.Errorf("finished = %q, want %q", finished, "the answer")
	}
}

// --- RunAgent: nil callbacks safe ---

func TestRunAgent_NilCallbacksSafe(t *testing.T) {
	stub := &tcStubClient{
		toolCalls: []llm.ToolCall{
			{Name: "getDateTime", Arguments: json.RawMessage(`{}`)},
		},
	}
	_, err := RunAgent(context.Background(), stub, "test", nil, AgentCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error with nil callbacks: %v", err)
	}
}
