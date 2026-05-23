package registry

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// stubAgent is a minimal agent.Agent implementation used to populate the
// registry in tests. It returns the canned responses configured on it.
type stubAgent struct {
	id    string
	name  string
	caps  []string
	resps []agent.Message
}

func (s *stubAgent) ID() string             { return s.id }
func (s *stubAgent) Name() string           { return s.name }
func (s *stubAgent) Capabilities() []string { return s.caps }
func (s *stubAgent) HandleMessage(_ context.Context, _ agent.Message, _ agent.Environment) ([]agent.Message, error) {
	return s.resps, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewInMemory()
	ctx := context.Background()

	a := &stubAgent{id: "a1", name: "Alpha", caps: []string{"summarize"}}
	if err := r.Register(ctx, a); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := r.Get(ctx, "a1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != "a1" || got.Name() != "Alpha" {
		t.Fatalf("unexpected agent: id=%q name=%q", got.ID(), got.Name())
	}
}

func TestRegistry_GetReturnsErrForUnknownID(t *testing.T) {
	r := NewInMemory()

	_, err := r.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for unknown id, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRegistry_ListReturnsAllRegistered(t *testing.T) {
	r := NewInMemory()
	ctx := context.Background()

	agents := []*stubAgent{
		{id: "a1", name: "Alpha"},
		{id: "a2", name: "Beta"},
		{id: "a3", name: "Gamma"},
	}
	for _, a := range agents {
		if err := r.Register(ctx, a); err != nil {
			t.Fatalf("Register %s: %v", a.id, err)
		}
	}

	got := r.List(ctx)
	if len(got) != len(agents) {
		t.Fatalf("List size: got %d, want %d", len(got), len(agents))
	}

	gotIDs := make([]string, 0, len(got))
	for _, a := range got {
		gotIDs = append(gotIDs, a.ID())
	}
	sort.Strings(gotIDs)
	want := []string{"a1", "a2", "a3"}
	for i, id := range want {
		if gotIDs[i] != id {
			t.Fatalf("List ids: got %v, want %v", gotIDs, want)
		}
	}
}

func TestRegistry_FindByCapabilityReturnsMatches(t *testing.T) {
	r := NewInMemory()
	ctx := context.Background()

	a1 := &stubAgent{id: "a1", caps: []string{"summarize", "translate"}}
	a2 := &stubAgent{id: "a2", caps: []string{"summarize"}}
	a3 := &stubAgent{id: "a3", caps: []string{"plan"}}
	for _, a := range []*stubAgent{a1, a2, a3} {
		if err := r.Register(ctx, a); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	got := r.FindByCapability(ctx, "summarize")
	if len(got) != 2 {
		t.Fatalf("expected 2 summarize agents, got %d", len(got))
	}
	ids := []string{got[0].ID(), got[1].ID()}
	sort.Strings(ids)
	if ids[0] != "a1" || ids[1] != "a2" {
		t.Fatalf("expected [a1 a2], got %v", ids)
	}
}

func TestRegistry_FindByCapabilityEmpty(t *testing.T) {
	r := NewInMemory()
	ctx := context.Background()

	if err := r.Register(ctx, &stubAgent{id: "a1", caps: []string{"summarize"}}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got := r.FindByCapability(ctx, "nope")
	if got == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(got))
	}
}

func TestRegistry_RegisterSameIDTwiceReplaces(t *testing.T) {
	r := NewInMemory()
	ctx := context.Background()

	first := &stubAgent{id: "a1", name: "First", caps: []string{"v1"}}
	second := &stubAgent{id: "a1", name: "Second", caps: []string{"v2"}}

	if err := r.Register(ctx, first); err != nil {
		t.Fatalf("Register first: %v", err)
	}
	if err := r.Register(ctx, second); err != nil {
		t.Fatalf("Register second: %v", err)
	}

	got, err := r.Get(ctx, "a1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name() != "Second" {
		t.Fatalf("expected replacement: got name=%q", got.Name())
	}
	if len(r.List(ctx)) != 1 {
		t.Fatalf("expected 1 registered agent after replace, got %d", len(r.List(ctx)))
	}
}
