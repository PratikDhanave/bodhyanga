// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package multiturn

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// -- ToolOrderCorrect ---------------------------------------------------------

func TestToolOrderCorrect_NoConstraint(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile", "writeFile"}}
	target := &MultiTurnTarget{} // ExpectedToolOrder empty → always 1.0
	if got := ToolOrderCorrect(result, target); got != 1.0 {
		t.Errorf("want 1.0, got %v", got)
	}
}

func TestToolOrderCorrect_Exact(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile", "writeFile"}}
	target := &MultiTurnTarget{ExpectedToolOrder: []string{"readFile", "writeFile"}}
	if got := ToolOrderCorrect(result, target); got != 1.0 {
		t.Errorf("want 1.0, got %v", got)
	}
}

func TestToolOrderCorrect_ExtraCallsAllowed(t *testing.T) {
	// Agent called an extra tool after the required sequence — still passes.
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile", "writeFile", "listDir"}}
	target := &MultiTurnTarget{ExpectedToolOrder: []string{"readFile", "writeFile"}}
	if got := ToolOrderCorrect(result, target); got != 1.0 {
		t.Errorf("want 1.0 for extra calls after expected sequence, got %v", got)
	}
}

func TestToolOrderCorrect_WrongOrder(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"writeFile", "readFile"}}
	target := &MultiTurnTarget{ExpectedToolOrder: []string{"readFile", "writeFile"}}
	if got := ToolOrderCorrect(result, target); got != 0.0 {
		t.Errorf("want 0.0 for wrong order, got %v", got)
	}
}

func TestToolOrderCorrect_TooFewCalls(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile"}}
	target := &MultiTurnTarget{ExpectedToolOrder: []string{"readFile", "writeFile"}}
	if got := ToolOrderCorrect(result, target); got != 0.0 {
		t.Errorf("want 0.0 when agent made fewer calls than expected, got %v", got)
	}
}

func TestToolOrderCorrect_EmptyResult(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{}}
	target := &MultiTurnTarget{ExpectedToolOrder: []string{"readFile"}}
	if got := ToolOrderCorrect(result, target); got != 0.0 {
		t.Errorf("want 0.0 when no calls made, got %v", got)
	}
}

// -- ToolsAvoided -------------------------------------------------------------

func TestToolsAvoided_NoConstraint(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile"}}
	target := &MultiTurnTarget{} // ForbiddenTools empty → always 1.0
	if got := ToolsAvoided(result, target); got != 1.0 {
		t.Errorf("want 1.0, got %v", got)
	}
}

func TestToolsAvoided_NoForbiddenCalled(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile"}}
	target := &MultiTurnTarget{ForbiddenTools: []string{"deleteFile", "runCommand"}}
	if got := ToolsAvoided(result, target); got != 1.0 {
		t.Errorf("want 1.0 when forbidden tools not called, got %v", got)
	}
}

func TestToolsAvoided_ForbiddenCalled(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile", "deleteFile"}}
	target := &MultiTurnTarget{ForbiddenTools: []string{"deleteFile"}}
	if got := ToolsAvoided(result, target); got != 0.0 {
		t.Errorf("want 0.0 when forbidden tool was called, got %v", got)
	}
}

func TestToolsAvoided_NoCallsMadeAtAll(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{}}
	target := &MultiTurnTarget{ForbiddenTools: []string{"readFile", "runCommand"}}
	if got := ToolsAvoided(result, target); got != 1.0 {
		t.Errorf("want 1.0 when no calls were made, got %v", got)
	}
}

func TestToolsAvoided_MultipleCallsOneForbidden(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile", "listDir", "runCommand"}}
	target := &MultiTurnTarget{ForbiddenTools: []string{"runCommand"}}
	if got := ToolsAvoided(result, target); got != 0.0 {
		t.Errorf("want 0.0 when any forbidden tool is called, got %v", got)
	}
}

// -- Combined evaluator pass/fail scenarios ----------------------------------

func TestCombined_FreshTaskSuccess(t *testing.T) {
	// Agent read the right file → order correct, no forbidden tools used.
	result := &MultiTurnResult{
		Text:          `The API endpoint is "https://api.example.com/v1".`,
		ToolCallOrder: []string{"readFile"},
	}
	target := &MultiTurnTarget{
		ExpectedToolOrder: []string{"readFile"},
		ForbiddenTools:    []string{"writeFile", "deleteFile"},
		OriginalTask:      "Read config.json and report the API endpoint",
		MockToolResults:   map[string]string{"readFile": `{"apiEndpoint":"https://api.example.com/v1"}`},
	}
	if got := ToolOrderCorrect(result, target); got != 1.0 {
		t.Errorf("ToolOrder want 1.0, got %v", got)
	}
	if got := ToolsAvoided(result, target); got != 1.0 {
		t.Errorf("ToolsAvoided want 1.0, got %v", got)
	}
}

func TestCombined_NegativeSimpleQuestion(t *testing.T) {
	// Agent correctly answered a math question without using any tools.
	result := &MultiTurnResult{
		Text:          "2 + 2 = 4",
		ToolCallOrder: []string{},
	}
	target := &MultiTurnTarget{
		ForbiddenTools: []string{"readFile", "runCommand", "writeFile"},
		OriginalTask:   "What is 2 + 2?",
	}
	if got := ToolsAvoided(result, target); got != 1.0 {
		t.Errorf("ToolsAvoided want 1.0, got %v", got)
	}
}

// ---------- LLMJudge ---------------------------------------------------------

// judgeMock is a test LLM client that returns a fixed response.
type judgeMock struct {
	response *llm.Response
	err      error
}

func (j *judgeMock) Provider() string { return "judge-mock" }
func (j *judgeMock) Model() string    { return "judge-mock-model" }
func (j *judgeMock) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	if j.err != nil {
		return nil, j.err
	}
	return j.response, nil
}

func TestLLMJudge_HappyPath(t *testing.T) {
	judgeClient := &judgeMock{
		response: &llm.Response{
			Content:    `{"score":8,"reason":"good answer"}`,
			StopReason: "end_turn",
		},
	}
	result := &MultiTurnResult{
		Text:          "The answer is 42.",
		ToolCallOrder: []string{"readFile"},
	}
	target := &MultiTurnTarget{
		OriginalTask:    "What is the answer?",
		MockToolResults: map[string]string{"readFile": `{"answer":42}`},
	}

	score, err := LLMJudge(context.Background(), judgeClient, result, target)
	if err != nil {
		t.Fatalf("LLMJudge: %v", err)
	}
	if score != 0.8 {
		t.Errorf("score: got %f, want 0.8", score)
	}
}

func TestLLMJudge_PerfectScore(t *testing.T) {
	judgeClient := &judgeMock{
		response: &llm.Response{
			Content:    `{"score":10,"reason":"perfect"}`,
			StopReason: "end_turn",
		},
	}
	result := &MultiTurnResult{Text: "x"}
	target := &MultiTurnTarget{OriginalTask: "q"}

	score, err := LLMJudge(context.Background(), judgeClient, result, target)
	if err != nil {
		t.Fatalf("LLMJudge: %v", err)
	}
	if score != 1.0 {
		t.Errorf("score: got %f, want 1.0", score)
	}
}

func TestLLMJudge_MinimumScore(t *testing.T) {
	judgeClient := &judgeMock{
		response: &llm.Response{
			Content:    `{"score":1,"reason":"terrible"}`,
			StopReason: "end_turn",
		},
	}
	result := &MultiTurnResult{Text: "x"}
	target := &MultiTurnTarget{OriginalTask: "q"}

	score, err := LLMJudge(context.Background(), judgeClient, result, target)
	if err != nil {
		t.Fatalf("LLMJudge: %v", err)
	}
	if score != 0.1 {
		t.Errorf("score: got %f, want 0.1", score)
	}
}

func TestLLMJudge_ClampLow(t *testing.T) {
	// Score below 1 should be clamped to 1.
	judgeClient := &judgeMock{
		response: &llm.Response{
			Content:    `{"score":0,"reason":"off-spec low"}`,
			StopReason: "end_turn",
		},
	}
	result := &MultiTurnResult{Text: "x"}
	target := &MultiTurnTarget{OriginalTask: "q"}

	score, err := LLMJudge(context.Background(), judgeClient, result, target)
	if err != nil {
		t.Fatalf("LLMJudge: %v", err)
	}
	// 0 is clamped to 1, so score = 1/10 = 0.1
	if score != 0.1 {
		t.Errorf("score: got %f, want 0.1 (clamped from 0)", score)
	}
}

func TestLLMJudge_ClampHigh(t *testing.T) {
	// Score above 10 should be clamped to 10.
	judgeClient := &judgeMock{
		response: &llm.Response{
			Content:    `{"score":15,"reason":"off-spec high"}`,
			StopReason: "end_turn",
		},
	}
	result := &MultiTurnResult{Text: "x"}
	target := &MultiTurnTarget{OriginalTask: "q"}

	score, err := LLMJudge(context.Background(), judgeClient, result, target)
	if err != nil {
		t.Fatalf("LLMJudge: %v", err)
	}
	if score != 1.0 {
		t.Errorf("score: got %f, want 1.0 (clamped from 15)", score)
	}
}

func TestLLMJudge_LLMError(t *testing.T) {
	judgeClient := &judgeMock{
		err: context.DeadlineExceeded,
	}
	result := &MultiTurnResult{Text: "x"}
	target := &MultiTurnTarget{OriginalTask: "q"}

	_, err := LLMJudge(context.Background(), judgeClient, result, target)
	if err == nil {
		t.Fatal("expected error from judge LLM")
	}
}

func TestLLMJudge_InvalidJSON(t *testing.T) {
	judgeClient := &judgeMock{
		response: &llm.Response{
			Content:    "not json at all",
			StopReason: "end_turn",
		},
	}
	result := &MultiTurnResult{Text: "x"}
	target := &MultiTurnTarget{OriginalTask: "q"}

	_, err := LLMJudge(context.Background(), judgeClient, result, target)
	if err == nil {
		t.Fatal("expected error for invalid JSON from judge")
	}
}

// ---------- mustMarshal -------------------------------------------------------

func TestMustMarshal_Success(t *testing.T) {
	b := mustMarshal(map[string]int{"a": 1})
	if len(b) == 0 {
		t.Fatal("expected non-empty result")
	}
}

func TestMustMarshal_JudgeSchemaIsValid(t *testing.T) {
	// Verify the pre-computed schema bytes are valid JSON.
	var m map[string]any
	if err := json.Unmarshal(judgeSchemaBytes, &m); err != nil {
		t.Fatalf("judgeSchemaBytes is invalid JSON: %v", err)
	}
	if m["type"] != "object" {
		t.Errorf("schema type: got %v, want 'object'", m["type"])
	}
}

// ---------- MultiTurnWithMocks — system message in prefilled messages --------

func TestMultiTurnWithMocks_SystemMessage(t *testing.T) {
	client := &seqMock{
		responses: []*llm.Response{
			newTextResp("Done"),
		},
	}

	data := MultiTurnEvalData{
		Messages: []InputMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello"},
		},
		MockTools: map[string]MockTool{},
	}

	result, err := MultiTurnWithMocks(context.Background(), client, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Done" {
		t.Errorf("text: got %q, want 'Done'", result.Text)
	}

	// The system message should have been extracted as the system prompt.
	if len(client.calls) != 1 {
		t.Fatalf("calls: got %d, want 1", len(client.calls))
	}
	if client.calls[0].System != "You are a helpful assistant." {
		t.Errorf("system: got %q", client.calls[0].System)
	}
	// Only the user message should be in the messages array.
	if len(client.calls[0].Messages) != 1 {
		t.Errorf("messages: got %d, want 1 (user only)", len(client.calls[0].Messages))
	}
}

// ---------- MultiTurnWithMocks — tool call with arguments ----------

func TestMultiTurnWithMocks_ToolCallWithArgs(t *testing.T) {
	client := &seqMock{
		responses: []*llm.Response{
			{
				StopReason: "tool_use",
				ToolCalls: []llm.ToolCall{
					{ID: "toolu_1", Name: "readFile", Arguments: json.RawMessage(`{"path":"config.json"}`)},
				},
			},
			newTextResp("Read it."),
		},
	}

	data := MultiTurnEvalData{
		Prompt: "Read config.json",
		MockTools: map[string]MockTool{
			"readFile": {Description: "Read a file", Result: "contents"},
		},
	}

	result, err := MultiTurnWithMocks(context.Background(), client, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Check that the args were recorded.
	if len(result.Steps) < 1 || len(result.Steps[0].ToolCalls) == 0 {
		t.Fatal("expected at least one step with tool calls")
	}
	args := result.Steps[0].ToolCalls[0].Args
	if args["path"] != "config.json" {
		t.Errorf("args: got %v", args)
	}
}

// ---------- MultiTurnWithMocks — deduplicate ToolsUsed ----------

// ---------- MultiTurnWithMocks — LLM error during loop ----------

func TestMultiTurnWithMocks_LLMErrorDuringStep(t *testing.T) {
	client := &errMock{err: context.DeadlineExceeded}
	data := MultiTurnEvalData{
		Prompt:    "Do something",
		MockTools: map[string]MockTool{},
	}
	_, err := MultiTurnWithMocks(context.Background(), client, data)
	if err == nil {
		t.Fatal("expected error from LLM")
	}
}

// errMock is an LLM client that always returns an error.
type errMock struct{ err error }

func (e *errMock) Provider() string { return "err-mock" }
func (e *errMock) Model() string    { return "err-mock-model" }
func (e *errMock) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return nil, e.err
}

func TestMultiTurnWithMocks_ToolsUsedDeduplicated(t *testing.T) {
	client := &seqMock{
		responses: []*llm.Response{
			newToolCallResp("t1", "readFile"),
			newToolCallResp("t2", "readFile"),
			newTextResp("done"),
		},
	}

	data := MultiTurnEvalData{
		Prompt: "Read files",
		MockTools: map[string]MockTool{
			"readFile": {Description: "Read", Result: "data"},
		},
	}

	result, err := MultiTurnWithMocks(context.Background(), client, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolsUsed) != 1 {
		t.Errorf("ToolsUsed: got %v, want [readFile]", result.ToolsUsed)
	}
	if len(result.ToolCallOrder) != 2 {
		t.Errorf("ToolCallOrder: got %v, want 2 entries", result.ToolCallOrder)
	}
}
