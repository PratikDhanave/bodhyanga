// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package multiturn

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

// seqMock is a test LLM client that returns responses from a pre-configured
// sequence. This lets executor tests simulate multi-turn tool-call loops
// without real API calls.
type seqMock struct {
	responses []*llm.Response
	calls     []*llm.Request
	pos       int
}

func (s *seqMock) Provider() string { return "seq-mock" }
func (s *seqMock) Model() string    { return "seq-mock-model" }
func (s *seqMock) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	clone := req
	s.calls = append(s.calls, &clone)
	if s.pos >= len(s.responses) {
		return &llm.Response{Content: "out of responses", StopReason: "end_turn"}, nil
	}
	resp := s.responses[s.pos]
	s.pos++
	return resp, nil
}

// newToolCallResp builds a mock response that calls the named tool with the
// given ID. The ID is required for Anthropic-style tool_result correlation.
func newToolCallResp(id, name string) *llm.Response {
	return &llm.Response{
		StopReason: "tool_use",
		ToolCalls:  []llm.ToolCall{{ID: id, Name: name}},
	}
}

// newTextResp builds a mock final-response with text and no tool calls.
func newTextResp(text string) *llm.Response {
	return &llm.Response{Content: text, StopReason: "end_turn"}
}

// --------------------------------------------------------------------------

func TestMultiTurnWithMocks_SingleToolCall(t *testing.T) {
	// Scenario: agent calls readFile once, then gives a final answer.
	client := &seqMock{
		responses: []*llm.Response{
			newToolCallResp("toolu_1", "readFile"),
			newTextResp(`The API endpoint is "https://api.example.com/v1".`),
		},
	}

	data := MultiTurnEvalData{
		Prompt: "Read config.json and tell me the API endpoint.",
		MockTools: map[string]MockTool{
			"readFile": {
				Description: "Read file contents",
				Result:      `{"apiEndpoint":"https://api.example.com/v1"}`,
			},
		},
	}

	result, err := MultiTurnWithMocks(context.Background(), client, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have recorded the readFile call.
	if len(result.ToolCallOrder) != 1 || result.ToolCallOrder[0] != "readFile" {
		t.Errorf("want ToolCallOrder=[readFile], got %v", result.ToolCallOrder)
	}

	if result.Text == "" {
		t.Error("want non-empty final text")
	}

	// Should have made two LLM calls (one for tool, one for final answer).
	if len(client.calls) != 2 {
		t.Errorf("want 2 LLM calls, got %d", len(client.calls))
	}

	// Second call must include the assistant message with tool calls +
	// user message with tool results (tool_result correlation).
	secondReq := client.calls[1]
	if len(secondReq.Messages) < 3 {
		t.Errorf("second request should have >=3 messages (initial+assistant+tool_result), got %d",
			len(secondReq.Messages))
	}

	// The assistant message (index 1) must carry the tool call.
	assistantMsg := secondReq.Messages[1]
	if len(assistantMsg.ToolCalls) == 0 {
		t.Error("assistant message in second request must carry ToolCalls")
	}
	if assistantMsg.ToolCalls[0].ID != "toolu_1" {
		t.Errorf("want ToolCall.ID=toolu_1, got %q", assistantMsg.ToolCalls[0].ID)
	}

	// The user message (index 2) must carry the tool result.
	userMsg := secondReq.Messages[2]
	if len(userMsg.ToolResults) == 0 {
		t.Error("user message in second request must carry ToolResults")
	}
	if userMsg.ToolResults[0].ToolUseID != "toolu_1" {
		t.Errorf("want ToolResult.ToolUseID=toolu_1, got %q", userMsg.ToolResults[0].ToolUseID)
	}
	if userMsg.ToolResults[0].Content == "" {
		t.Error("want non-empty tool result content")
	}
}

func TestMultiTurnWithMocks_TwoTools(t *testing.T) {
	// Scenario: agent calls readFile then writeFile, then gives final answer.
	client := &seqMock{
		responses: []*llm.Response{
			newToolCallResp("toolu_1", "readFile"),
			newToolCallResp("toolu_2", "writeFile"),
			newTextResp("I've updated the config file with the new port."),
		},
	}

	data := MultiTurnEvalData{
		Prompt: "Update the port in config.json to 3000.",
		MockTools: map[string]MockTool{
			"readFile":  {Description: "Read file", Result: `{"port":8080}`},
			"writeFile": {Description: "Write file", Result: "Written successfully"},
		},
	}

	result, err := MultiTurnWithMocks(context.Background(), client, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"readFile", "writeFile"}
	if len(result.ToolCallOrder) != 2 {
		t.Fatalf("want 2 tool calls, got %v", result.ToolCallOrder)
	}
	for i, w := range want {
		if result.ToolCallOrder[i] != w {
			t.Errorf("ToolCallOrder[%d]: want %q, got %q", i, w, result.ToolCallOrder[i])
		}
	}

	if len(result.ToolsUsed) != 2 {
		t.Errorf("want 2 unique tools used, got %d", len(result.ToolsUsed))
	}

	// Three LLM calls: readFile turn, writeFile turn, final answer.
	if len(client.calls) != 3 {
		t.Errorf("want 3 LLM calls, got %d", len(client.calls))
	}
}

func TestMultiTurnWithMocks_NoToolCalls(t *testing.T) {
	// Scenario: agent answers directly without calling any tools.
	client := &seqMock{
		responses: []*llm.Response{
			newTextResp("2 + 2 = 4"),
		},
	}

	data := MultiTurnEvalData{
		Prompt: "What is 2 + 2?",
		MockTools: map[string]MockTool{
			"calculator": {Description: "Evaluate an expression", Result: "4"},
		},
	}

	result, err := MultiTurnWithMocks(context.Background(), client, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCallOrder) != 0 {
		t.Errorf("want no tool calls, got %v", result.ToolCallOrder)
	}
	if result.Text == "" {
		t.Error("want non-empty final text")
	}
	if len(client.calls) != 1 {
		t.Errorf("want 1 LLM call, got %d", len(client.calls))
	}
}

func TestMultiTurnWithMocks_PrefilledMessages(t *testing.T) {
	// Scenario: pre-filled conversation history (mid-conversation start).
	client := &seqMock{
		responses: []*llm.Response{
			newToolCallResp("toolu_1", "readFile"),
			newTextResp("I'll update config.json with port 3000."),
		},
	}

	data := MultiTurnEvalData{
		Messages: []InputMessage{
			{Role: "user", Content: "I need to update a config file"},
			{Role: "assistant", Content: "Sure, which file and what changes?"},
			{Role: "user", Content: "Change port to 3000 in config.json"},
		},
		MockTools: map[string]MockTool{
			"readFile":  {Description: "Read file", Result: `{"port":8080}`},
			"writeFile": {Description: "Write file", Result: "Written successfully"},
		},
	}

	if _, err := MultiTurnWithMocks(context.Background(), client, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The first LLM call should have 3 pre-filled messages.
	if len(client.calls[0].Messages) != 3 {
		t.Errorf("want 3 initial messages, got %d", len(client.calls[0].Messages))
	}
}

func TestMultiTurnWithMocks_MaxSteps(t *testing.T) {
	// Scenario: agent keeps calling tools but we cap at MaxSteps=2.
	client := &seqMock{
		responses: []*llm.Response{
			newToolCallResp("toolu_1", "readFile"),
			newToolCallResp("toolu_2", "readFile"),
			// This third response should NOT be reached.
			newTextResp("should not reach here"),
		},
	}

	data := MultiTurnEvalData{
		Prompt: "Read every file",
		MockTools: map[string]MockTool{
			"readFile": {Description: "Read a file", Result: "contents"},
		},
		Config: &EvalConfig{MaxSteps: 2},
	}

	result, err := MultiTurnWithMocks(context.Background(), client, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 2 steps run: both are tool calls. Final text is empty because the
	// loop ended before the agent gave a text response.
	if len(result.Steps) != 2 {
		t.Errorf("want 2 steps, got %d", len(result.Steps))
	}
	if len(client.calls) != 2 {
		t.Errorf("want 2 LLM calls (MaxSteps=2), got %d", len(client.calls))
	}
}

func TestMultiTurnWithMocks_UnknownTool(t *testing.T) {
	// Scenario: agent calls a tool not in the mock map; result should be "".
	client := &seqMock{
		responses: []*llm.Response{
			newToolCallResp("toolu_1", "unknownTool"),
			newTextResp("OK"),
		},
	}

	data := MultiTurnEvalData{
		Prompt: "Do something",
		MockTools: map[string]MockTool{
			"readFile": {Description: "Read", Result: "data"},
		},
	}

	result, err := MultiTurnWithMocks(context.Background(), client, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tool result content should be empty string for unknown tool.
	if len(result.Steps) == 0 || len(result.Steps[0].ToolResults) == 0 {
		t.Fatal("want at least one step with a tool result")
	}
	if result.Steps[0].ToolResults[0].Result != "" {
		t.Errorf("want empty result for unknown tool, got %q",
			result.Steps[0].ToolResults[0].Result)
	}
}

func TestMultiTurnWithMocks_NeitherPromptNorMessages(t *testing.T) {
	// Scenario: no prompt and no messages → should error.
	client := &seqMock{responses: []*llm.Response{newTextResp("ok")}}
	data := MultiTurnEvalData{
		MockTools: map[string]MockTool{},
	}

	if _, err := MultiTurnWithMocks(context.Background(), client, data); err == nil {
		t.Error("want error when neither Prompt nor Messages set")
	}
}
