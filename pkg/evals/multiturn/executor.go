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
	"encoding/json"
	"fmt"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

const (
	defaultMaxSteps = 20
)

// mockToolSchema is the JSON Schema used for every mocked tool. We accept any
// object argument — the mock ignores args and returns a fixed result.
var mockToolSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path":    {"type": "string"},
		"content": {"type": "string"},
		"args":    {"type": "string"}
	},
	"additionalProperties": true
}`)

// MultiTurnWithMocks runs the agent loop using agentClient, with every tool
// call intercepted and answered by a fixed mock response.
//
// Execution:
//  1. Build tool definitions from data.MockTools.
//  2. Call the LLM with the current message history + tools.
//  3. If the response contains tool calls, look up their mock results, append
//     assistant turn (with tool_use blocks) + user turn (with tool_result
//     blocks) to the conversation, then repeat.
//  4. When the LLM responds with no tool calls, return the final text.
//
// The loop is capped at data.Config.MaxSteps (default 20) to prevent runaway
// agents from exhausting API quota.
func MultiTurnWithMocks(ctx context.Context, agentClient llm.Client, data MultiTurnEvalData) (*MultiTurnResult, error) {
	maxSteps := defaultMaxSteps
	if data.Config != nil && data.Config.MaxSteps > 0 {
		maxSteps = data.Config.MaxSteps
	}

	// Build tool definitions from the mock map. The schema accepts any args;
	// the mock returns a fixed result regardless.
	tools := make([]llm.Tool, 0, len(data.MockTools))
	for name, mock := range data.MockTools {
		tools = append(tools, llm.Tool{
			Name:        name,
			Description: mock.Description,
			Schema:      mockToolSchema,
		})
	}

	// Build initial conversation from either pre-filled messages or a fresh prompt.
	var systemPrompt string
	messages := make([]llm.Message, 0, 4)

	if len(data.Messages) > 0 {
		for _, m := range data.Messages {
			if m.Role == "system" {
				systemPrompt = m.Content
				continue
			}
			messages = append(messages, llm.Message{
				Role:    llm.Role(m.Role),
				Content: m.Content,
			})
		}
	} else if data.Prompt != "" {
		messages = append(messages, llm.Message{
			Role:    llm.RoleUser,
			Content: data.Prompt,
		})
	} else {
		return nil, fmt.Errorf("multiturn: eval data must have either Prompt or Messages")
	}

	var (
		steps         []Step
		allToolCalls  []string
		finalText     string
	)

	for stepIdx := 0; stepIdx < maxSteps; stepIdx++ {
		req := llm.Request{
			System:    systemPrompt,
			Messages:  messages,
			MaxTokens: 1024,
			Tools:     tools,
		}

		resp, err := agentClient.Complete(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("multiturn: step %d: %w", stepIdx, err)
		}

		s := Step{Text: resp.Content}

		// No tool calls → agent is done.
		if len(resp.ToolCalls) == 0 {
			finalText = resp.Content
			steps = append(steps, s)
			break
		}

		// Process each tool call: record it and return the mock result.
		stepCalls := make([]StepToolCall, 0, len(resp.ToolCalls))
		stepResults := make([]StepToolResult, 0, len(resp.ToolCalls))
		toolResults := make([]llm.ToolResult, 0, len(resp.ToolCalls))

		for _, tc := range resp.ToolCalls {
			allToolCalls = append(allToolCalls, tc.Name)

			var args map[string]any
			if len(tc.Arguments) > 0 {
				_ = json.Unmarshal(tc.Arguments, &args)
			}
			stepCalls = append(stepCalls, StepToolCall{ToolName: tc.Name, Args: args})

			// Return the fixed mock result (empty string for unknown tools).
			result := ""
			if mock, ok := data.MockTools[tc.Name]; ok {
				result = mock.Result
			}
			stepResults = append(stepResults, StepToolResult{ToolName: tc.Name, Result: result})

			// ToolUseID must match tc.ID so the provider can correlate
			// the tool_result block in the next turn.
			toolResults = append(toolResults, llm.ToolResult{
				ToolUseID: tc.ID,
				Content:   result,
			})
		}

		s.ToolCalls = stepCalls
		s.ToolResults = stepResults
		steps = append(steps, s)

		// Extend conversation:
		//   - Assistant turn: carries the tool_use blocks (and any text).
		//   - User turn: carries the tool_result blocks.
		// Both are required by Anthropic's API for subsequent turns.
		messages = append(messages,
			llm.Message{
				Role:      llm.RoleAssistant,
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls, // serialized as tool_use blocks
			},
			llm.Message{
				Role:        llm.RoleUser,
				ToolResults: toolResults, // serialized as tool_result blocks
			},
		)
	}

	// Deduplicate tool names for ToolsUsed (preserve first-seen order).
	seen := make(map[string]bool, len(allToolCalls))
	toolsUsed := make([]string, 0, len(allToolCalls))
	for _, name := range allToolCalls {
		if !seen[name] {
			seen[name] = true
			toolsUsed = append(toolsUsed, name)
		}
	}

	return &MultiTurnResult{
		Text:          finalText,
		Steps:         steps,
		ToolsUsed:     toolsUsed,
		ToolCallOrder: allToolCalls,
	}, nil
}
