package computeruse

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/tools"
)

const (
	// DefaultModel is the default Ollama model for this lesson.
	DefaultModel = "llama3.2"

	// MaxIterations caps the tool-calling loop to avoid infinite cycles.
	MaxIterations = 10

	// SystemPrompt is the persona injected into every computer-use run.
	SystemPrompt = `You are a helpful assistant with access to computer-use tools.

Available tools:
- open_url: fetch a web page and read its text content
- run_command: run safe, read-only shell commands (ls, pwd, cat, head, tail, wc, grep, find, go, git status/log/diff)
- take_screenshot: capture the current display (headless stub — returns unavailable)

When the user asks you to find information from the web, use open_url to fetch the relevant page.
When the user asks about local files or the project structure, use run_command.
Always use the tools to gather real information before synthesising your answer.
Cite the URLs you visited or commands you ran in your final response.`
)

// RunAgent drives the computer-use tool loop until the model returns a final
// text response or MaxIterations is reached.
//
// Parameters:
//   - ctx      — request context (cancellation propagates to HTTP calls and commands)
//   - client   — the LLM backend
//   - message  — the user's question or task description
//   - maxIter  — maximum number of tool-calling rounds; 0 uses MaxIterations
//   - cbs      — optional lifecycle hooks; nil or nil fields are ignored
func RunAgent(
	ctx context.Context,
	client llm.Client,
	message string,
	maxIter int,
	cbs *AgentCallbacks,
) (string, error) {
	if maxIter <= 0 {
		maxIter = MaxIterations
	}

	if cbs != nil && cbs.OnStart != nil {
		cbs.OnStart(message)
	}

	registry := tools.NewComputerRegistry()

	req := llm.Request{
		System: SystemPrompt,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: message},
		},
		Tools:       registry.LLMTools(),
		MaxTokens:   2048,
		Temperature: 0.1,
	}

	for i := 0; i < maxIter; i++ {
		resp, err := client.Complete(ctx, req)
		if err != nil {
			return "", fmt.Errorf("computeruse: iteration %d: %w", i+1, err)
		}

		// No tool calls — the model returned a final text response.
		if len(resp.ToolCalls) == 0 {
			answer := strings.TrimSpace(resp.Content)
			if cbs != nil && cbs.OnFinish != nil {
				cbs.OnFinish(answer)
			}
			return answer, nil
		}

		// Append the assistant turn (with tool calls) to the conversation.
		req.Messages = append(req.Messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each tool call and collect results.
		toolResults := make([]llm.ToolResult, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			if cbs != nil && cbs.OnComputerAction != nil {
				cbs.OnComputerAction(tc.Name, tc.Arguments)
			}

			resultJSON := invokeToolSafe(ctx, registry, tc)

			if cbs != nil && cbs.OnToolResult != nil {
				cbs.OnToolResult(tc.Name, resultJSON)
			}

			toolResults = append(toolResults, llm.ToolResult{
				ToolUseID: tc.ID,
				Content:   string(resultJSON),
			})
		}

		// Feed the tool results back as a user turn.
		req.Messages = append(req.Messages, llm.Message{
			Role:        llm.RoleUser,
			ToolResults: toolResults,
		})
	}

	return "", fmt.Errorf("computeruse: reached max iterations (%d) without a final response", maxIter)
}

// invokeToolSafe dispatches a single tool call and returns a JSON result.
// If the tool is unknown or returns an error, a JSON error document is
// returned so the conversation can continue without an unrecoverable panic.
func invokeToolSafe(ctx context.Context, reg *tools.Registry, tc llm.ToolCall) json.RawMessage {
	t, ok := reg.Get(tc.Name)
	if !ok {
		msg, _ := json.Marshal(map[string]string{
			"error": fmt.Sprintf("unknown tool %q", tc.Name),
		})
		return msg
	}

	result, err := t.Invoke(ctx, tc.Arguments)
	if err != nil {
		msg, _ := json.Marshal(map[string]string{
			"error": err.Error(),
			"tool":  tc.Name,
		})
		return msg
	}
	return result
}
