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
	"fmt"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// ToolOrderCorrect returns 1.0 if the agent's actual tool-call sequence begins
// with the expected sequence, 0.0 otherwise. An empty ExpectedToolOrder always
// returns 1.0 (no constraint imposed).
//
// "Begins with" means the first len(expected) calls must match in order; the
// agent is free to make additional calls afterward. Use this when the eval only
// cares that mandatory steps happen first.
func ToolOrderCorrect(result *MultiTurnResult, target *MultiTurnTarget) float64 {
	if len(target.ExpectedToolOrder) == 0 {
		return 1.0
	}
	if len(result.ToolCallOrder) < len(target.ExpectedToolOrder) {
		return 0.0
	}
	for i, expected := range target.ExpectedToolOrder {
		if result.ToolCallOrder[i] != expected {
			return 0.0
		}
	}
	return 1.0
}

// ToolsAvoided returns 1.0 if none of the forbidden tools were called, 0.0 if
// any forbidden tool appears in the call order. An empty ForbiddenTools list
// always returns 1.0.
func ToolsAvoided(result *MultiTurnResult, target *MultiTurnTarget) float64 {
	forbidden := make(map[string]bool, len(target.ForbiddenTools))
	for _, t := range target.ForbiddenTools {
		forbidden[t] = true
	}
	for _, t := range result.ToolCallOrder {
		if forbidden[t] {
			return 0.0
		}
	}
	return 1.0
}

// judgeOutput is the structured output schema for the LLM judge.
type judgeOutput struct {
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

// judgeSchemaBytes is the JSON Schema for judgeOutput.
var judgeSchemaBytes = mustMarshal(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"score": map[string]any{
			"type":        "integer",
			"minimum":     1,
			"maximum":     10,
			"description": "Score from 1-10 where 10 is perfect",
		},
		"reason": map[string]any{
			"type":        "string",
			"description": "Brief explanation for the score",
		},
	},
	"required": []string{"score", "reason"},
})

// LLMJudge uses a strong LLM to evaluate the semantic quality of the agent's
// final response. Returns 0.0–1.0 (internally uses a 1–10 scale divided by 10).
//
// The judge receives:
//   - The original task description
//   - Which tools were called and in what order
//   - The mock tool results that were available to the agent
//   - The agent's final text response
//
// This catches failures that deterministic evaluators miss: the agent called
// the right tools but misread the results, or gave an answer that's technically
// correct but doesn't address the user's question.
func LLMJudge(ctx context.Context, judgeClient llm.Client, result *MultiTurnResult, target *MultiTurnTarget) (float64, error) {
	toolsCalledJSON, _ := json.Marshal(result.ToolCallOrder)
	toolResultsJSON, _ := json.Marshal(target.MockToolResults)

	userContent := fmt.Sprintf(
		"Task: %s\n\nTools called (in order): %s\nTool results available: %s\n\nAgent's final response:\n%s\n\nEvaluate whether the response correctly uses the tool results to address the task.",
		target.OriginalTask,
		string(toolsCalledJSON),
		string(toolResultsJSON),
		result.Text,
	)

	req := llm.Request{
		System: `You are an evaluation judge assessing an AI agent's response quality.
Score the response on a scale of 1-10 using these criteria:

10: Fully addresses the task, correctly interprets and uses all tool results
7-9: Mostly correct with minor inaccuracies or incomplete reasoning
4-6: Partially addresses the task; some tool results correctly used
1-3: Mostly incorrect, misinterprets results, or irrelevant to the task

If no tools were called and none were needed (simple question), score based on
the response quality alone. If tools were needed but not called, score 1-3.`,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: userContent},
		},
		MaxTokens: 512,
		JSONSchema: &llm.JSONSchema{
			Name:        "evaluation",
			Description: "Evaluation of an AI agent response quality",
			Schema:      judgeSchemaBytes,
		},
	}

	resp, err := judgeClient.Complete(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("llm judge: %w", err)
	}

	var grade judgeOutput
	if err := json.Unmarshal([]byte(resp.Content), &grade); err != nil {
		return 0, fmt.Errorf("llm judge: decode response: %w", err)
	}

	// Clamp to valid range just in case the model goes off-spec.
	score := grade.Score
	if score < 1 {
		score = 1
	}
	if score > 10 {
		score = 10
	}
	return float64(score) / 10.0, nil
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("multiturn: mustMarshal: " + err.Error())
	}
	return b
}
