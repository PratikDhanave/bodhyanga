// Package agentloop — run.go
//
// TypeScript equivalent: the RunAgent function in the "03-Agent-Loop" lesson.
// This is the core of the lesson: a tool-calling loop that:
//  1. Sends the request to the LLM with available tools.
//  2. If the model returns tool calls, executes each tool and feeds the
//     results back as ToolResult messages.
//  3. Repeats until the model returns a plain text response or MaxIterations
//     is reached.
package agentloop

import (
	"context"
	"fmt"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

const (
	// DefaultModel is the default Ollama model for this lesson.
	DefaultModel = "qwen2.5:7b"

	// MaxIterations caps the agent loop to prevent infinite tool-call cycles.
	MaxIterations = 10

	// SystemPrompt is the assistant persona used across all lesson runs.
	SystemPrompt = "You are a helpful assistant. When the user asks about the current time or date, use the getDateTime tool. When the user asks about weather, use the getWeather tool. Always use the available tools to answer questions that require real-time information."
)

// RunAgent runs the agent loop until the model produces a final text response
// or MaxIterations is reached. cbs may be nil or have nil fields — hooks that
// are nil are simply skipped.
func RunAgent(ctx context.Context, client llm.Client, message string, maxIter int, cbs *AgentCallbacks) (string, error) {
	if maxIter <= 0 {
		maxIter = MaxIterations
	}

	if cbs != nil && cbs.OnStart != nil {
		cbs.OnStart(message)
	}

	// Seed the conversation with the user's message.
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: message},
	}

	for i := 0; i < maxIter; i++ {
		resp, err := client.Complete(ctx, llm.Request{
			System:    SystemPrompt,
			Messages:  messages,
			MaxTokens: 1024,
			Tools:     Tools(),
		})
		if err != nil {
			return "", fmt.Errorf("agentloop: iteration %d: %w", i+1, err)
		}

		// No tool calls — the model returned a final text response.
		if len(resp.ToolCalls) == 0 {
			if cbs != nil && cbs.OnFinish != nil {
				cbs.OnFinish(resp.Content)
			}
			return resp.Content, nil
		}

		// The model wants to call one or more tools.
		// 1. Append the assistant message (with tool calls) to history.
		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// 2. Execute each tool call and collect results.
		toolResults := make([]llm.ToolResult, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			if cbs != nil && cbs.OnToolCall != nil {
				cbs.OnToolCall(tc.Name, tc.Arguments)
			}

			result, execErr := ExecuteTool(tc.Name, tc.Arguments)
			if execErr != nil {
				result = fmt.Sprintf("error: %s", execErr.Error())
			}

			if cbs != nil && cbs.OnToolResult != nil {
				cbs.OnToolResult(tc.Name, result)
			}

			toolResults = append(toolResults, llm.ToolResult{
				ToolUseID: tc.ID,
				Content:   result,
			})
		}

		// 3. Append the tool results as a user turn so the model can continue.
		messages = append(messages, llm.Message{
			Role:        llm.RoleUser,
			ToolResults: toolResults,
		})
	}

	return "", fmt.Errorf("agentloop: reached max iterations (%d) without a final response", maxIter)
}
