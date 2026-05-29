// Package intro implements the simplest possible AI agent: an LLM call with
// no tools, no loop, and no persistent memory. It is the Go translation of
// the TypeScript "01-Intro-to-Agents" lesson.
//
// What makes something an agent (and what this lesson lacks):
//
//   - LLM   ✓  — uses pkg/llm.Client to send prompts and receive completions
//   - Actions ✗  — no tools; the model can only produce text
//   - Loop   ✗  — responds once and stops (future lessons add the loop)
//
// The AgentCallbacks type mirrors the TypeScript AgentCallbacks in
// src/types.ts. All fields are optional; nil hooks are ignored.
package intro

// AgentCallbacks provides optional lifecycle hooks for a basic agent run.
// Callers can use these to add logging, streaming output, or metrics without
// modifying RunAgent.
//
// TypeScript equivalent: AgentCallbacks in src/types.ts
type AgentCallbacks struct {
	// OnStart is called immediately before the LLM request is sent.
	// userMessage is the raw input the user provided.
	OnStart func(userMessage string)

	// OnFinish is called after the LLM returns its response.
	// response is the full text content returned by the model.
	OnFinish func(response string)
}
