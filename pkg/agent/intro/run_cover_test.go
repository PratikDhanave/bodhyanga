package intro

import (
	"context"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// --- stub client ---

type introStubClient struct {
	response string
	err      error
}

func (s *introStubClient) Provider() string { return "stub" }
func (s *introStubClient) Model() string    { return "stub-model" }
func (s *introStubClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &llm.Response{Content: s.response}, nil
}

// --- RunAgent LLM error ---

func TestRunAgent_LLMError(t *testing.T) {
	stub := &introStubClient{err: errors.New("llm error")}
	_, err := RunAgent(context.Background(), stub, "hello", nil, AgentCallbacks{})
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

// --- RunAgent with history ---

func TestRunAgent_WithHistory(t *testing.T) {
	stub := &introStubClient{response: "response with history"}
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "previous question"},
		{Role: llm.RoleAssistant, Content: "previous answer"},
	}
	got, err := RunAgent(context.Background(), stub, "follow-up", history, AgentCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "response with history" {
		t.Errorf("got %q, want %q", got, "response with history")
	}
}

// --- RunAgent callbacks ---

func TestRunAgent_AllCallbacksFire(t *testing.T) {
	stub := &introStubClient{response: "the answer"}
	var started, finished string
	cb := AgentCallbacks{
		OnStart:  func(msg string) { started = msg },
		OnFinish: func(resp string) { finished = resp },
	}
	got, err := RunAgent(context.Background(), stub, "question", nil, cb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "the answer" {
		t.Errorf("got %q, want %q", got, "the answer")
	}
	if started != "question" {
		t.Errorf("OnStart called with %q, want %q", started, "question")
	}
	if finished != "the answer" {
		t.Errorf("OnFinish called with %q, want %q", finished, "the answer")
	}
}

// --- RunAgent nil callbacks ---

func TestRunAgent_NilCallbacksSafe(t *testing.T) {
	stub := &introStubClient{response: "ok"}
	_, err := RunAgent(context.Background(), stub, "hello", nil, AgentCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error with nil callbacks: %v", err)
	}
}
