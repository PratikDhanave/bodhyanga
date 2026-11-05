package toolcalling

import (
	"encoding/json"
	"fmt"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

// localTool bundles an llm.Tool descriptor with its Execute function.
// The descriptor is advertised to the model; Execute is called by
// ExecuteTool when the model selects the tool.
//
// TypeScript equivalent: the object returned by Vercel AI SDK's tool() helper,
// which packages description, parameters, and execute together.
type localTool struct {
	llm.Tool

	// Execute is the local implementation. args is the decoded JSON object
	// the model sent (may be empty for zero-argument tools). Returns the
	// tool's output as a plain string (or JSON) that is fed back to the model
	// as a tool_result in the next conversation turn.
	Execute func(args map[string]any) (string, error)
}

// registry is the canonical set of tools available in this lesson.
// Add entries here to expose new tools to the agent.
//
// TypeScript equivalent: the tools: { getDateTime } object passed to
// generateText / streamText.
var registry = map[string]*localTool{
	"getDateTime": &getDateTimeTool,
}

// Tools returns the llm.Tool slice to pass in llm.Request.Tools.
// The slice is built from the registry on each call — order is not
// guaranteed, which is fine because LLM providers don't rely on order.
//
// TypeScript equivalent: the tools object spread into the generateText call.
func Tools() []llm.Tool {
	out := make([]llm.Tool, 0, len(registry))
	for _, t := range registry {
		out = append(out, t.Tool)
	}
	return out
}

// ExecuteTool dispatches a tool call by name, decoding the raw JSON arguments
// the model produced and invoking the matching localTool.Execute.
//
// Returns the tool output string on success, or an error if the tool is
// unknown or execution fails. The caller wraps the output in an
// llm.ToolResult{ToolUseID: ..., Content: output} to continue the
// multi-turn conversation.
//
// In lesson 02 ExecuteTool is defined but not yet called inside RunAgent —
// it is here so later lessons (03, 04) can import and use it. The full
// "call → execute → append → continue" loop appears in lesson 04.
//
// TypeScript equivalent: the execute function injected into each tool() call;
// the Vercel AI SDK invokes it automatically during streamText.
func ExecuteTool(name string, argsJSON json.RawMessage) (string, error) {
	t, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}

	// Decode the arguments JSON produced by the model. Zero-argument tools
	// produce "{}" or "null" — both decode to an empty (or nil) map, which
	// we normalise to an empty map so Execute always receives a non-nil map.
	var args map[string]any
	if len(argsJSON) > 0 && string(argsJSON) != "null" {
		if err := json.Unmarshal(argsJSON, &args); err != nil {
			return "", fmt.Errorf("decode args for tool %q: %w", name, err)
		}
	}
	if args == nil {
		args = map[string]any{}
	}

	return t.Execute(args)
}
