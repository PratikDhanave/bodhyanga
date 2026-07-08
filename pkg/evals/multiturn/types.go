// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Package multiturn provides a multi-turn agent evaluation framework.
//
// # Overview
//
// Single-turn evals test one prompt → one response. Multi-turn evals test the
// full agent loop: does the agent accomplish the task across multiple tool calls
// and reasoning steps?
//
// # Architecture
//
//	executor  — runs the agent loop against mocked tools (deterministic, no I/O)
//	evaluators — score the result: tool order, forbidden tools, LLM-as-judge
//	runner    — loads a JSON dataset, runs executor + evaluators, reports scores
//
// # Why Mocked Tools?
//
// Mocked tools return fixed values so evals are:
//   - Reproducible: same input → same tool results every run
//   - Fast: no file system, network, or LLM tool implementations
//   - Safe: can't accidentally delete files or call external APIs
//   - Controllable: easy to inject error scenarios ("file not found")
//
// This tests the agent's decision-making (which tools to call, in what order,
// how to interpret results) without testing the tool implementations themselves.
//
// # LLM-as-Judge
//
// Because multi-turn output is non-deterministic (the agent might reach the
// right answer via different wording), we use a strong judge model to evaluate
// semantic correctness. The judge receives the original task, the tool results
// that were available, and the agent's final response, and returns a 1–10 score.
// The judge uses structured output (JSONSchema) for reliable parsing.
package multiturn

// MockTool defines one tool available in an eval test case.
// Result is the fixed value returned whenever the model calls this tool,
// regardless of what arguments the model supplies.
type MockTool struct {
	Description string `json:"description"`
	Result      string `json:"result"`
}

// MultiTurnEvalData is the input for a single eval test case.
// Either Prompt (fresh task) or Messages (mid-conversation) must be set.
type MultiTurnEvalData struct {
	// Prompt is the user's first message. Used when testing fresh tasks.
	// Mutually exclusive with Messages.
	Prompt string `json:"prompt,omitempty"`

	// Messages is a pre-filled conversation history. Used when testing
	// mid-conversation continuation behavior. Roles: "system", "user",
	// "assistant". Mutually exclusive with Prompt.
	Messages []InputMessage `json:"messages,omitempty"`

	// MockTools defines the tools available to the agent and their fixed
	// return values. Keys are tool names (e.g. "readFile").
	MockTools map[string]MockTool `json:"mockTools"`

	// Config overrides execution parameters. Zero value uses defaults.
	Config *EvalConfig `json:"config,omitempty"`
}

// InputMessage is one turn in a pre-filled conversation history.
type InputMessage struct {
	Role    string `json:"role"`    // "system", "user", or "assistant"
	Content string `json:"content"` // message text
}

// EvalConfig controls how the executor runs one test case.
type EvalConfig struct {
	// Model overrides the default judge/agent model.
	Model string `json:"model,omitempty"`
	// MaxSteps caps the number of agent turns. Default 20.
	MaxSteps int `json:"maxSteps,omitempty"`
}

// MultiTurnTarget defines the expected behavior for one eval case.
// All fields are optional; omit a field to skip that evaluator.
type MultiTurnTarget struct {
	// ExpectedToolOrder is the exact tool-call sequence the agent must
	// follow. [] means no constraint (evaluator always returns 1.0).
	ExpectedToolOrder []string `json:"expectedToolOrder,omitempty"`

	// ForbiddenTools lists tools the agent must NOT call. If the agent
	// calls any, toolsAvoided returns 0.0. [] means no constraint.
	ForbiddenTools []string `json:"forbiddenTools,omitempty"`

	// OriginalTask is the human-readable description of what the user
	// asked for, passed to the LLM judge as context.
	OriginalTask string `json:"originalTask"`

	// MockToolResults maps tool name → result string, used by the judge
	// to know what information was available to the agent.
	MockToolResults map[string]string `json:"mockToolResults,omitempty"`
}

// Step records one LLM turn in the multi-turn agent loop.
type Step struct {
	// ToolCalls lists the tools the model invoked in this step.
	ToolCalls []StepToolCall `json:"toolCalls,omitempty"`
	// ToolResults lists the (mocked) results returned for each call.
	ToolResults []StepToolResult `json:"toolResults,omitempty"`
	// Text is the model's text content for this step (may be empty if
	// the model only emitted tool_use blocks).
	Text string `json:"text,omitempty"`
}

// StepToolCall records one tool invocation within a Step.
type StepToolCall struct {
	ToolName string         `json:"toolName"`
	Args     map[string]any `json:"args,omitempty"` // arguments the model supplied
}

// StepToolResult records one tool response within a Step.
type StepToolResult struct {
	ToolName string `json:"toolName"`
	Result   string `json:"result"` // mocked return value
}

// MultiTurnResult is the complete output of executing one eval case.
type MultiTurnResult struct {
	// Text is the agent's final text response (after all tool calls resolved).
	Text string `json:"text"`
	// Steps is the full turn-by-turn execution trace.
	Steps []Step `json:"steps"`
	// ToolsUsed is the deduplicated set of tool names that were called.
	ToolsUsed []string `json:"toolsUsed"`
	// ToolCallOrder is every tool name in call order (with duplicates).
	ToolCallOrder []string `json:"toolCallOrder"`
}

// EvalCase is one entry in the eval dataset JSON file.
type EvalCase struct {
	Data   MultiTurnEvalData `json:"data"`
	Target MultiTurnTarget   `json:"target"`
}

// EvalScore holds all evaluator results for one test case.
type EvalScore struct {
	// CaseIndex is the 0-based position in the dataset.
	CaseIndex int `json:"caseIndex"`
	// ToolOrder is 1.0 if tool call order matched, 0.0 otherwise.
	ToolOrder float64 `json:"toolOrder"`
	// ToolsAvoided is 1.0 if no forbidden tools were called, 0.0 otherwise.
	ToolsAvoided float64 `json:"toolsAvoided"`
	// OutputQuality is the LLM judge score, 0.0–1.0.
	OutputQuality float64 `json:"outputQuality"`
	// Overall is the unweighted average of all three scores.
	Overall float64 `json:"overall"`
	// Result holds the execution trace. May be nil on error.
	Result *MultiTurnResult `json:"result,omitempty"`
	// Error records any execution error. Empty on success.
	Error string `json:"error,omitempty"`
}
