package intro

import (
	"context"
	"fmt"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

// DefaultModel is the default Ollama model used when no model is specified.
// Matches the project-wide default in the Makefile (OLLAMA_MODEL).
const DefaultModel = "qwen2.5:7b"

// RunAgent sends userMessage to the LLM and returns its response.
//
// This is the simplest possible "agent" — really just an LLM call.
// It is NOT a true agent yet because:
//
//   - No tools  — the model can describe actions but cannot execute them.
//   - No loop   — it responds once and stops. A real agent loops until the
//     task is complete (see lesson 03).
//   - No memory — history is accepted but not used in this lesson. Later
//     lessons thread conversation history through to enable multi-turn
//     dialogue (see lesson 02).
//
// TypeScript equivalent: runAgent() in src/agent/run.ts
//
// Parameters:
//   - client   — the LLM backend (Ollama, Anthropic, etc.) constructed by
//     the caller. Use llm.NewOllama or llm.NewAnthropic from pkg/llm.
//   - userMessage — the raw user input string (equivalent to the `prompt`
//     parameter in the TypeScript generateText call).
//   - history  — prior conversation turns, earliest first. Empty slice or
//     nil means a fresh conversation. The conversationHistory parameter is
//     carried forward so the signature matches later lessons that use it.
//   - callbacks — optional lifecycle hooks (OnStart, OnFinish). Nil fields
//     are ignored.
//
// Returns the assistant's text response, or an error if the LLM call fails.
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

	// Build the message list: prior turns (if any) followed by the new
	// user message. In this lesson history is always empty; lesson 02 adds
	// conversation memory.
	messages := make([]llm.Message, 0, len(history)+1)
	messages = append(messages, history...)
	messages = append(messages, llm.Message{
		Role:    llm.RoleUser,
		Content: userMessage,
	})

	// The single LLM call — equivalent to:
	//   const { text } = await generateText({ model, prompt, system })
	// in the TypeScript lesson.
	resp, err := client.Complete(ctx, llm.Request{
		System:    SystemPrompt,
		Messages:  messages,
		MaxTokens: 1024,
		// Temperature deliberately unset (0.0 = provider default, usually
		// deterministic). Lesson exercises suggest changing this to observe
		// the effect on response variability.
	})
	if err != nil {
		return "", fmt.Errorf("intro agent: %w", err)
	}

	if callbacks.OnFinish != nil {
		callbacks.OnFinish(resp.Content)
	}

	return resp.Content, nil
}
