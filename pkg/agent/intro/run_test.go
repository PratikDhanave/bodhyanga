package intro_test

import (
	"context"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent/intro"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

// stubClient is a deterministic llm.Client for testing. It records the
// Request and returns a fixed Response. No network calls, no API keys.
type stubClient struct {
	response string
	last     *llm.Request
}

func (s *stubClient) Provider() string { return "stub" }
func (s *stubClient) Model() string    { return "stub-model" }
func (s *stubClient) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	s.last = &req
	return &llm.Response{
		Content:    s.response,
		Provider:   "stub",
		Model:      "stub-model",
		StopReason: "stop",
	}, nil
}

func TestRunAgent_ReturnsLLMContent(t *testing.T) {
	stub := &stubClient{response: "An agent is an LLM that can take actions in a loop."}
	got, err := intro.RunAgent(context.Background(), stub, "What is an agent?", nil, intro.AgentCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != stub.response {
		t.Errorf("want %q, got %q", stub.response, got)
	}
}

func TestRunAgent_SendsSystemPrompt(t *testing.T) {
	stub := &stubClient{response: "ok"}
	_, _ = intro.RunAgent(context.Background(), stub, "hello", nil, intro.AgentCallbacks{})
	if stub.last == nil {
		t.Fatal("Complete was not called")
	}
	if stub.last.System == "" {
		t.Error("want a non-empty system prompt, got empty string")
	}
	// The system prompt should contain the word "assistant" (from SystemPrompt).
	if !strings.Contains(stub.last.System, "assistant") {
		t.Errorf("system prompt should mention 'assistant', got: %q", stub.last.System)
	}
}

func TestRunAgent_UserMessageIsLastInHistory(t *testing.T) {
	stub := &stubClient{response: "ok"}
	userMsg := "What is the ReAct pattern?"
	_, _ = intro.RunAgent(context.Background(), stub, userMsg, nil, intro.AgentCallbacks{})
	if stub.last == nil {
		t.Fatal("Complete was not called")
	}
	if len(stub.last.Messages) == 0 {
		t.Fatal("expected at least one message in request")
	}
	last := stub.last.Messages[len(stub.last.Messages)-1]
	if last.Role != llm.RoleUser {
		t.Errorf("want last message role=user, got %s", last.Role)
	}
	if last.Content != userMsg {
		t.Errorf("want last message content=%q, got %q", userMsg, last.Content)
	}
}

func TestRunAgent_PrependsHistory(t *testing.T) {
	// Although lesson 01 doesn't use history, the signature accepts it
	// so lessons 02+ can add memory without changing the call site.
	stub := &stubClient{response: "ok"}
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "previous turn"},
		{Role: llm.RoleAssistant, Content: "previous answer"},
	}
	_, _ = intro.RunAgent(context.Background(), stub, "follow-up question", history, intro.AgentCallbacks{})
	if stub.last == nil {
		t.Fatal("Complete was not called")
	}
	// Expect history (2) + new user message (1) = 3 messages.
	if len(stub.last.Messages) != 3 {
		t.Errorf("want 3 messages (history+new), got %d", len(stub.last.Messages))
	}
	if stub.last.Messages[0].Content != "previous turn" {
		t.Errorf("first message should be the first history turn, got %q",
			stub.last.Messages[0].Content)
	}
}

func TestRunAgent_CallbacksAreFired(t *testing.T) {
	stub := &stubClient{response: "agent response"}
	var startMsg, finishMsg string
	cb := intro.AgentCallbacks{
		OnStart:  func(msg string) { startMsg = msg },
		OnFinish: func(resp string) { finishMsg = resp },
	}
	input := "callback test"
	_, _ = intro.RunAgent(context.Background(), stub, input, nil, cb)
	if startMsg != input {
		t.Errorf("OnStart: want %q, got %q", input, startMsg)
	}
	if finishMsg != stub.response {
		t.Errorf("OnFinish: want %q, got %q", stub.response, finishMsg)
	}
}

func TestRunAgent_NilCallbacksAreSafe(t *testing.T) {
	stub := &stubClient{response: "ok"}
	// All callback fields nil — should not panic.
	_, err := intro.RunAgent(context.Background(), stub, "hello", nil, intro.AgentCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error with nil callbacks: %v", err)
	}
}

func TestRunAgent_MaxTokensIsSet(t *testing.T) {
	stub := &stubClient{response: "ok"}
	_, _ = intro.RunAgent(context.Background(), stub, "hello", nil, intro.AgentCallbacks{})
	if stub.last == nil {
		t.Fatal("Complete was not called")
	}
	if stub.last.MaxTokens <= 0 {
		t.Errorf("want MaxTokens > 0, got %d", stub.last.MaxTokens)
	}
}
