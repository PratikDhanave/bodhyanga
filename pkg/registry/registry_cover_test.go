package registry

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
)

// --- Register: nil agent ---

func TestRegister_NilAgent(t *testing.T) {
	r := NewInMemory()
	err := r.Register(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil agent")
	}
}

// --- Register: empty ID agent ---

type emptyIDAgent struct{}

func (e emptyIDAgent) ID() string            { return "" }
func (e emptyIDAgent) Name() string          { return "empty" }
func (e emptyIDAgent) Capabilities() []string { return nil }
func (e emptyIDAgent) HandleMessage(_ context.Context, _ agent.Message, _ agent.Environment) ([]agent.Message, error) {
	return nil, nil
}

func TestRegister_EmptyID(t *testing.T) {
	r := NewInMemory()
	err := r.Register(context.Background(), emptyIDAgent{})
	if err == nil {
		t.Fatal("expected error for empty ID agent")
	}
}
