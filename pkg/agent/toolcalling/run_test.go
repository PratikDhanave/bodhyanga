package toolcalling_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent/toolcalling"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

// ── stub client ──────────────────────────────────────────────────────────────

// stubClient is a deterministic llm.Client for testing. It records the Request
// and returns a fixed Response. No network calls, no API keys.
type stubClient struct {
	response string
	toolCall *llm.ToolCall // if set, Complete returns this tool call instead of text
	err      error
	last     *llm.Request
}

func (s *stubClient) Provider() string { return "stub" }
func (s *stubClient) Model() string    { return "stub-model" }
func (s *stubClient) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	s.last = &req
	if s.err != nil {
		return nil, s.err
	}
	if s.toolCall != nil {
		return &llm.Response{
			Provider:   "stub",
			Model:      "stub-model",
			StopReason: "tool_use",
			ToolCalls:  []llm.ToolCall{*s.toolCall},
		}, nil
	}
	return &llm.Response{
		Content:    s.response,
		Provider:   "stub",
		Model:      "stub-model",
		StopReason: "stop",
	}, nil
}

// ── RunAgent tests ────────────────────────────────────────────────────────────

func TestRunAgent_TextResponse(t *testing.T) {
	want := "The agent can call tools when it needs real-time information."
	stub := &stubClient{response: want}

	got, err := toolcalling.RunAgent(
		context.Background(), stub,
		"What can this agent do?", nil,
		toolcalling.AgentCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestRunAgent_SendsSystemPrompt(t *testing.T) {
	stub := &stubClient{response: "ok"}
	_, _ = toolcalling.RunAgent(context.Background(), stub, "hello", nil, toolcalling.AgentCallbacks{})

	if stub.last == nil {
		t.Fatal("Complete was not called")
	}
	if stub.last.System == "" {
		t.Error("want a non-empty system prompt, got empty string")
	}
	if !strings.Contains(stub.last.System, "tools") {
		t.Errorf("system prompt should mention 'tools', got: %q", stub.last.System)
	}
}

func TestRunAgent_AdvertisesTools(t *testing.T) {
	stub := &stubClient{response: "ok"}
	_, _ = toolcalling.RunAgent(context.Background(), stub, "hello", nil, toolcalling.AgentCallbacks{})

	if stub.last == nil {
		t.Fatal("Complete was not called")
	}
	if len(stub.last.Tools) == 0 {
		t.Error("want at least one tool advertised, got none")
	}

	// The getDateTime tool must be present.
	found := false
	for _, tool := range stub.last.Tools {
		if tool.Name == "getDateTime" {
			found = true
			break
		}
	}
	if !found {
		t.Error("want getDateTime in advertised tools")
	}
}

func TestRunAgent_ToolCallFiresCallback(t *testing.T) {
	tc := &llm.ToolCall{
		ID:        "toolu_001",
		Name:      "getDateTime",
		Arguments: json.RawMessage(`{}`),
	}
	stub := &stubClient{toolCall: tc}

	var cbName string
	var cbArgs json.RawMessage
	callbacks := toolcalling.AgentCallbacks{
		OnToolCall: func(name string, args json.RawMessage) {
			cbName = name
			cbArgs = args
		},
	}

	result, err := toolcalling.RunAgent(context.Background(), stub, "What time is it?", nil, callbacks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cbName != "getDateTime" {
		t.Errorf("OnToolCall: want name %q, got %q", "getDateTime", cbName)
	}
	if string(cbArgs) != `{}` {
		t.Errorf("OnToolCall: want args %q, got %q", `{}`, string(cbArgs))
	}
	// The result should mention the tool name.
	if !strings.Contains(result, "getDateTime") {
		t.Errorf("result should mention tool name, got %q", result)
	}
}

func TestRunAgent_OnStartAndOnFinishCalled(t *testing.T) {
	stub := &stubClient{response: "42"}
	var started, finished bool
	callbacks := toolcalling.AgentCallbacks{
		OnStart:  func(_ string) { started = true },
		OnFinish: func(_ string) { finished = true },
	}

	_, err := toolcalling.RunAgent(context.Background(), stub, "hi", nil, callbacks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !started {
		t.Error("OnStart was not called")
	}
	if !finished {
		t.Error("OnFinish was not called")
	}
}

func TestRunAgent_ErrorPropagates(t *testing.T) {
	stub := &stubClient{err: errors.New("network timeout")}
	_, err := toolcalling.RunAgent(context.Background(), stub, "hello", nil, toolcalling.AgentCallbacks{})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "network timeout") {
		t.Errorf("want error to contain 'network timeout', got %q", err.Error())
	}
}

func TestRunAgent_HistoryIncluded(t *testing.T) {
	stub := &stubClient{response: "ok"}
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "previous question"},
		{Role: llm.RoleAssistant, Content: "previous answer"},
	}

	_, _ = toolcalling.RunAgent(context.Background(), stub, "follow-up", history, toolcalling.AgentCallbacks{})

	if stub.last == nil {
		t.Fatal("Complete was not called")
	}
	// History + new user message = 3 messages total.
	if len(stub.last.Messages) != 3 {
		t.Errorf("want 3 messages, got %d", len(stub.last.Messages))
	}
	last := stub.last.Messages[len(stub.last.Messages)-1]
	if last.Content != "follow-up" {
		t.Errorf("last message should be follow-up, got %q", last.Content)
	}
}

// ── Tools() / ExecuteTool tests ───────────────────────────────────────────────

func TestTools_ContainsGetDateTime(t *testing.T) {
	tools := toolcalling.Tools()
	if len(tools) == 0 {
		t.Fatal("Tools() returned empty slice")
	}
	for _, tool := range tools {
		if tool.Name == "getDateTime" {
			return // found
		}
	}
	t.Error("want getDateTime in Tools()")
}

func TestExecuteTool_GetDateTime(t *testing.T) {
	before := time.Now().UTC().Add(-2 * time.Second)
	result, err := toolcalling.ExecuteTool("getDateTime", json.RawMessage(`{}`))
	after := time.Now().UTC().Add(2 * time.Second)

	if err != nil {
		t.Fatalf("ExecuteTool: unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("ExecuteTool: empty result")
	}

	// Result should parse as RFC 3339 and fall within the execution window.
	parsed, parseErr := time.Parse(time.RFC3339, result)
	if parseErr != nil {
		t.Fatalf("ExecuteTool: result %q is not RFC 3339: %v", result, parseErr)
	}
	if parsed.Before(before) || parsed.After(after) {
		t.Errorf("ExecuteTool: time %v outside window [%v, %v]", parsed, before, after)
	}
}

func TestExecuteTool_GetDateTimeNullArgs(t *testing.T) {
	// Some providers return "null" instead of "{}" for empty-argument tools.
	_, err := toolcalling.ExecuteTool("getDateTime", json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("ExecuteTool with null args: unexpected error: %v", err)
	}
}

func TestExecuteTool_UnknownTool(t *testing.T) {
	_, err := toolcalling.ExecuteTool("notATool", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want error for unknown tool, got nil")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("want 'unknown tool' in error, got %q", err.Error())
	}
}
