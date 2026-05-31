// Package toolcalling implements the "02-Tool-Calling" lesson: an agent that
// can advertise tools to the LLM, observe which tools the model selects, and
// prepare to execute them. It is the Go translation of the TypeScript
// "02-Tool-Calling" lesson.
//
// What this lesson adds on top of lesson 01 (intro):
//
//   - Tools      ✓  — pkg/llm.Tool descriptors are passed in the request; the
//     model can choose to call them.
//   - Tool calls  ✓  — Response.ToolCalls is detected and surfaced via
//     AgentCallbacks.OnToolCall.
//   - ExecuteTool ✓  — the dispatch helper is defined here so lesson 03 can
//     call it inside the agent loop.
//   - Loop        ✗  — this lesson detects the first tool call and stops.
//     The full "call → execute → continue" loop is introduced in lesson 04.
//
// The AgentCallbacks type mirrors the TypeScript AgentCallbacks in
// src/types.ts. All fields are optional; nil hooks are ignored.
package toolcalling

import "encoding/json"

// AgentCallbacks provides optional lifecycle hooks for a tool-calling agent
// run. Callers use these for logging, streaming output, or metrics without
// modifying RunAgent.
//
// TypeScript equivalent: AgentCallbacks in src/types.ts
type AgentCallbacks struct {
	// OnStart is called immediately before the first LLM request is sent.
	// userMessage is the raw input the user provided.
	OnStart func(userMessage string)

	// OnToolCall is called each time the model asks to invoke a tool.
	// name is the tool name (e.g. "getDateTime"); args is the raw JSON
	// argument object the model produced.
	//
	// In this lesson the callback fires but the tool is not yet executed
	// inside RunAgent — that loop arrives in lesson 04.
	OnToolCall func(name string, args json.RawMessage)

	// OnFinish is called after a final text response is available.
	// response is the full text content returned by the model.
	OnFinish func(response string)
}
