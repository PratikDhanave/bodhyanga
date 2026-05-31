// Package computeruse implements the "10-Computer-Use" lesson: an agent that
// can control a browser/shell to complete tasks requiring interaction with
// external systems (web pages, forms, CLIs).
//
// Tools provided:
//
//   - open_url       — fetch a web page and return stripped text (≤4 KB)
//   - run_command    — run a safe, read-only shell command (≤4 KB stdout)
//   - take_screenshot — stub; returns {"available":false} in headless mode
//
// This package follows the same pattern as pkg/agent/agentloop: AgentCallbacks
// carries optional lifecycle hooks, and RunAgent drives the tool loop.
package computeruse

import "encoding/json"

// AgentCallbacks provides optional lifecycle hooks for a computer-use agent
// run. All fields are optional; nil hooks are ignored.
type AgentCallbacks struct {
	// OnStart is called with the user's message before the first LLM call.
	OnStart func(userMessage string)

	// OnComputerAction is called each time the agent invokes a computer-use
	// tool. name is the tool name ("open_url", "run_command",
	// "take_screenshot"); args is the raw JSON argument object.
	OnComputerAction func(name string, args json.RawMessage)

	// OnToolResult is called with the tool name and the raw JSON result
	// returned by the tool.
	OnToolResult func(name string, result json.RawMessage)

	// OnFinish is called with the agent's final text response.
	OnFinish func(response string)
}
