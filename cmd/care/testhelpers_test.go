package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/engagement"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/registry"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

// newTestAPIServer constructs an apiServer wired to fresh in-memory components
// for unit tests. fixturesDir is a per-test TempDir so test fixtures don't
// collide.
func newTestAPIServer(t *testing.T) (*apiServer, *comm.InMemoryBus, func()) {
	t.Helper()
	bus := comm.NewInMemoryBus()
	reg := registry.NewInMemory()
	queue := tasks.NewQueue()
	reviews := hitl.NewQueue()
	patients := medical.NewPatientRegistry()
	notifier := engagement.NewInMemory()
	traces := newLLMTraceBuffer(50)
	runs := newRunManager(bus)
	s := &apiServer{
		bus:         bus,
		reg:         reg,
		queue:       queue,
		reviews:     reviews,
		patients:    patients,
		notifier:    notifier,
		traces:      traces,
		runs:        runs,
		fixturesDir: t.TempDir(),
		provider:    "rule",
	}
	return s, bus, func() {}
}

// stubAgent is a minimal agent.Agent for registry tests.
type stubAgent struct {
	id   string
	caps []string
}

func (s stubAgent) ID() string             { return s.id }
func (s stubAgent) Name() string           { return s.id }
func (s stubAgent) Capabilities() []string { return s.caps }
func (s stubAgent) HandleMessage(_ context.Context, _ agent.Message, _ agent.Environment) ([]agent.Message, error) {
	return nil, nil
}

// writeFixture marshals a fixtureFile into dir/demo_<name>.json. Used by tests
// that exercise loadFixtures / loadFixtureByID / startRunHandler.
func writeFixture(t *testing.T, dir, name string, f fixtureFile) {
	t.Helper()
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	p := filepath.Join(dir, "demo_"+name+".json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

// waitForCondition polls fn every 5ms until it returns true or the deadline
// elapses. Returns true on success, false if the deadline was reached.
// This is the test-side replacement for ad-hoc time.Sleep.
func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) bool {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if fn() {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-tick.C:
		}
	}
}
