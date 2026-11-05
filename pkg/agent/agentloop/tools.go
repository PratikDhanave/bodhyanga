package agentloop

import (
	"encoding/json"
	"fmt"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

type localTool struct {
	llm.Tool
	Execute func(args map[string]any) (string, error)
}

var registry = map[string]*localTool{
	"getDateTime": &getDateTimeTool,
	"getWeather":  &getWeatherTool,
}

// Tools returns the llm.Tool slice for llm.Request.Tools.
func Tools() []llm.Tool {
	out := make([]llm.Tool, 0, len(registry))
	for _, t := range registry {
		out = append(out, t.Tool)
	}
	return out
}

// ExecuteTool dispatches a tool call by name.
func ExecuteTool(name string, argsJSON json.RawMessage) (string, error) {
	t, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	var args map[string]any
	if len(argsJSON) > 0 && string(argsJSON) != "null" {
		if err := json.Unmarshal(argsJSON, &args); err != nil {
			return "", fmt.Errorf("decode args for %q: %w", name, err)
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	return t.Execute(args)
}
