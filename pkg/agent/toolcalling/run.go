package toolcalling

import (
	"context"
	"fmt"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// DefaultModel is the default Ollama model used when no model is specified.
// Matches the project-wide default in the Makefile (OLLAMA_MODEL).
const DefaultModel = "qwen2.5:7b"

// RunAgent sends userMessage to the LLM with the full tool registry
// advertised and returns the agent's response.
//
// Lesson 02 progression over lesson 01:
//
//   - Tools are passed in llm.Request.Tools so the model may select a tool.
//   - If the model responds with tool calls, OnToolCall fires for each call
//     and RunAgent returns the tool names as a descriptive placeholder string.
//     (The real execution + loop arrives in lesson 04.)
//   - If the model responds with text, the text is returned as usual.
//
// TypeScript equivalent: runAgent() in src/agent/run.ts updated for lesson 02
// to pass the tools option to generateText / streamText.
//
// Parameters:
//   - client      — the LLM backend (Ollama, Anthropic, etc.)
//   - userMessage — the raw user input string
//   - history     — prior conversation turns, earliest first; nil = fresh
//   - callbacks   — optional lifecycle hooks; nil fields are ignored
//
// Returns the assistant's text response (or a tool-call summary if the model
// selected tools), or an error if the LLM call fails.
func RunAgent(
	ctx context.Context,
	client llm.Client,
	userMessage string,
	history []llm.Message,
	callbacks AgentCallbacks,
) (string, error) {
	if callbacks.OnStart != nil {
		callbacks.OnStart(userMessage)
	}

	// Build message list: prior turns + new user message.
	messages := make([]llm.Message, 0, len(history)+1)
	messages = append(messages, history...)
	messages = append(messages, llm.Message{
		Role:    llm.RoleUser,
		Content: userMessage,
	})

	// Send the request with the full tool registry advertised.
	// The model may either (a) call one or more tools, or (b) reply with text.
	//
	// TypeScript equivalent:
	//
	//   const result = await generateText({
	//       model,
	//       system:   SYSTEM_PROMPT,
	//       messages: conversationHistory,
	//       tools:    { getDateTime },
	//   })
	resp, err := client.Complete(ctx, llm.Request{
		System:    SystemPrompt,
		Messages:  messages,
		MaxTokens: 1024,
		Tools:     Tools(),
	})
	if err != nil {
		return "", fmt.Errorf("toolcalling agent: %w", err)
	}

	// ── Tool-call path ────────────────────────────────────────────────────
	// The model chose to call one or more tools instead of replying directly.
	// Fire OnToolCall for each selection so the caller can log / display the
	// choice, then return a descriptive placeholder.
	//
	// Lesson 02 does NOT execute the tools here. ExecuteTool is defined in
	// tools.go and is ready to use; lesson 04 wires it into a "call →
	// execute → append → continue" loop.
	if len(resp.ToolCalls) > 0 {
		names := make([]string, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			if callbacks.OnToolCall != nil {
				callbacks.OnToolCall(tc.Name, tc.Arguments)
			}
			names = append(names, tc.Name)
		}
		// Return a readable summary so the lesson CLI has something to print.
		summary := fmt.Sprintf("[tool calls requested: %s]", strings.Join(names, ", "))
		if callbacks.OnFinish != nil {
			callbacks.OnFinish(summary)
		}
		return summary, nil
	}

	// ── Text path ─────────────────────────────────────────────────────────
	// The model responded with text (no tool calls). This is the same path
	// as lesson 01.
	if callbacks.OnFinish != nil {
		callbacks.OnFinish(resp.Content)
	}
	return resp.Content, nil
}
