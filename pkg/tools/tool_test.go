package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeTool is a minimal Tool used only in registry tests so the registry
// behaviour is exercised without leaning on any of the concrete clinical
// tools (DrugInteraction etc.). Keeping it test-local also means a future
// refactor of the production tools cannot accidentally fix or break these
// tests.
type fakeTool struct {
	name string
	desc string
}

func (f fakeTool) Name() string                { return f.name }
func (f fakeTool) Description() string         { return f.desc }
func (f fakeTool) Schema() json.RawMessage      { return json.RawMessage(`{"type":"object"}`) }
func (f fakeTool) Invoke(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true}`), nil
}

func TestRegistry_GetReturnsRegisteredTool(t *testing.T) {
	r := NewRegistry(fakeTool{name: "alpha", desc: "a"}, fakeTool{name: "beta", desc: "b"})
	got, ok := r.Get("alpha")
	if !ok {
		t.Fatalf("Get(alpha): not found")
	}
	if got.Name() != "alpha" {
		t.Fatalf("Get(alpha).Name=%q, want alpha", got.Name())
	}
}

func TestRegistry_GetMissReturnsFalse(t *testing.T) {
	r := NewRegistry(fakeTool{name: "alpha"})
	if _, ok := r.Get("unknown"); ok {
		t.Fatalf("Get(unknown) should be a miss")
	}
}

func TestRegistry_ListIsSorted(t *testing.T) {
	// Insert out of order; List() must return alpha,beta,gamma.
	r := NewRegistry(fakeTool{name: "gamma"}, fakeTool{name: "alpha"}, fakeTool{name: "beta"})
	got := r.List()
	if len(got) != 3 {
		t.Fatalf("List len=%d, want 3", len(got))
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, t0 := range got {
		if t0.Name() != want[i] {
			t.Fatalf("List[%d]=%q, want %q (sort-stable)", i, t0.Name(), want[i])
		}
	}
}

func TestRegistry_LLMToolsConversionPreservesFields(t *testing.T) {
	r := NewRegistry(
		fakeTool{name: "alpha", desc: "first"},
		fakeTool{name: "beta", desc: "second"},
	)
	got := r.LLMTools()
	if len(got) != 2 {
		t.Fatalf("LLMTools len=%d, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[0].Description != "first" {
		t.Fatalf("LLMTools[0]=%+v, want alpha/first", got[0])
	}
	if got[1].Name != "beta" || got[1].Description != "second" {
		t.Fatalf("LLMTools[1]=%+v, want beta/second", got[1])
	}
	if string(got[0].Schema) != `{"type":"object"}` {
		t.Fatalf("LLMTools[0].Schema=%s, want object", string(got[0].Schema))
	}
}

func TestRegistry_LastWinsOnDuplicateName(t *testing.T) {
	// When two tools share a name the later one overrides — lets callers
	// build a default registry then swap one entry without touching the rest.
	r := NewRegistry(
		fakeTool{name: "alpha", desc: "first"},
		fakeTool{name: "alpha", desc: "second"},
	)
	got, _ := r.Get("alpha")
	if got.Description() != "second" {
		t.Fatalf("override failed: desc=%q, want second", got.Description())
	}
	if len(r.List()) != 1 {
		t.Fatalf("duplicate name should collapse to one entry, got %d", len(r.List()))
	}
}

func TestRegistry_NilSafeMethods(t *testing.T) {
	// A nil registry should not panic on read methods so an agent can fall
	// through to "no tools advertised" without a guard at every call site.
	var r *Registry
	if _, ok := r.Get("x"); ok {
		t.Fatalf("nil Get should miss")
	}
	if got := r.List(); got != nil {
		t.Fatalf("nil List should return nil, got %v", got)
	}
	if got := r.LLMTools(); got != nil {
		t.Fatalf("nil LLMTools should return nil, got %v", got)
	}
}

func TestRegistry_NilToolSkipped(t *testing.T) {
	// Passing a nil Tool in variadic args must not panic and must not produce
	// a zero entry that breaks downstream callers.
	r := NewRegistry(nil, fakeTool{name: "alpha"}, nil)
	if len(r.List()) != 1 {
		t.Fatalf("nil entries should be skipped, got len=%d", len(r.List()))
	}
}
