package tools

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// Tool is a callable, deterministic clinical utility the LLM may invoke
// during its reasoning. Implementations are expected to be pure (no I/O
// beyond what is documented), idempotent, and safe to call concurrently —
// the registry hands them out by name and the agent may invoke several in
// the same conversation round.
type Tool interface {
	// Name is the stable identifier the model references in a ToolCall. It
	// must be a valid identifier (snake_case, no spaces) so providers that
	// constrain tool names accept it.
	Name() string

	// Description is the short, human-readable summary the LLM sees as the
	// tool's purpose. Keep it under ~120 chars.
	Description() string

	// Schema is the JSON Schema (subset supported by pkg/llm) for the input
	// arguments. The schema is advertised to the model so it can construct
	// well-formed calls.
	Schema() json.RawMessage

	// Invoke runs the tool with the supplied JSON arguments and returns a
	// JSON document with the result. Errors are returned when the arguments
	// fail to parse or the tool cannot satisfy the request; the agent maps
	// errors to a synthetic JSON result so the conversation can continue.
	Invoke(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}

// Registry holds a set of Tools keyed by name. The zero value is not usable;
// construct one with NewRegistry. The registry is read-only after
// construction — concurrent reads are safe without locking.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry returns a Registry populated with the supplied tools. Later
// tools with the same Name() override earlier ones (last-wins) so callers
// can compose a default registry with a custom override.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		if t == nil {
			continue
		}
		r.tools[t.Name()] = t
	}
	return r
}

// Get looks up a tool by name. The boolean is false when no tool with that
// name is registered.
func (r *Registry) Get(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	t, ok := r.tools[name]
	return t, ok
}

// List returns the registered tools in deterministic (name-sorted) order so
// logs and test assertions are stable.
func (r *Registry) List() []Tool {
	if r == nil || len(r.tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Tool, 0, len(names))
	for _, n := range names {
		out = append(out, r.tools[n])
	}
	return out
}

// LLMTools converts every registered Tool into the llm.Tool descriptor that
// providers consume on Request.Tools. The returned slice is freshly
// allocated and may be mutated by the caller.
func (r *Registry) LLMTools() []llm.Tool {
	if r == nil || len(r.tools) == 0 {
		return nil
	}
	ts := r.List()
	out := make([]llm.Tool, 0, len(ts))
	for _, t := range ts {
		out = append(out, llm.Tool{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	return out
}
