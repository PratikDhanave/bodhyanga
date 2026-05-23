package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

// TestRefresh_SubscribesAgentsAddedAfterStart is the core hot-reload guarantee:
// an agent registered AFTER Start must receive messages once Refresh has been
// called. Without Refresh the bus has no subscription for that ID and the
// publish would silently drop on the floor.
func TestRefresh_SubscribesAgentsAddedAfterStart(t *testing.T) {
	first := &stubAgent{
		id:    "first",
		name:  "first",
		inbox: make(chan agent.Message, 1),
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, first)
	o.Start(context.Background())

	// Register a brand-new agent on the same registry the orchestrator holds.
	// buildOrchestrator constructed the registry; we reach it back through the
	// orchestrator to avoid duplicating wiring code.
	second := &stubAgent{
		id:    "second",
		name:  "second",
		inbox: make(chan agent.Message, 1),
	}
	if err := o.reg.Register(context.Background(), second); err != nil {
		t.Fatalf("register second: %v", err)
	}

	// Sanity check: without Refresh, a publish to "second" goes nowhere.
	bus.Publish(context.Background(), agent.NewMessage("user", "second", agent.RoleUser, "task", "pre-refresh", nil))
	if _, ok := waitFor(second.inbox, 100*time.Millisecond); ok {
		t.Fatal("second received a message before Refresh was called")
	}

	added := o.Refresh(context.Background())
	if len(added) != 1 || added[0] != "second" {
		t.Fatalf("Refresh returned %v, want [second]", added)
	}

	bus.Publish(context.Background(), agent.NewMessage("user", "second", agent.RoleUser, "task", "post-refresh", nil))
	got, ok := waitFor(second.inbox, 500*time.Millisecond)
	if !ok {
		t.Fatal("second did not receive message after Refresh")
	}
	if got.Content != "post-refresh" {
		t.Fatalf("unexpected delivery: %+v", got)
	}
}

// TestRefresh_DoesNotResubscribeExistingAgents ensures Refresh is idempotent:
// calling it twice with no new registrations must not double-subscribe the
// already-known agent, otherwise a single publish would invoke HandleMessage
// twice.
func TestRefresh_DoesNotResubscribeExistingAgents(t *testing.T) {
	worker := &stubAgent{
		id:    "worker",
		name:  "worker",
		inbox: make(chan agent.Message, 4),
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)
	o.Start(context.Background())

	if added := o.Refresh(context.Background()); len(added) != 0 {
		t.Fatalf("first Refresh returned %v, want empty", added)
	}
	if added := o.Refresh(context.Background()); len(added) != 0 {
		t.Fatalf("second Refresh returned %v, want empty", added)
	}

	bus.Publish(context.Background(), agent.NewMessage("user", "worker", agent.RoleUser, "task", "once", nil))

	// Wait for the first delivery, then give the bus extra grace to surface a
	// spurious second delivery if double-subscription occurred.
	if _, ok := waitFor(worker.inbox, 500*time.Millisecond); !ok {
		t.Fatal("worker never received the publish")
	}
	time.Sleep(150 * time.Millisecond)

	if got := worker.callCount(); got != 1 {
		t.Fatalf("HandleMessage called %d times, want 1 (no duplicates)", got)
	}
}

// TestRefresh_ReturnsNewlySubscribedIDs documents the return contract — the
// IDs of agents that were just subscribed, and only those. Existing agents
// are not echoed back.
func TestRefresh_ReturnsNewlySubscribedIDs(t *testing.T) {
	first := &stubAgent{id: "first", name: "first"}
	o, _, _ := buildOrchestrator(t, allowAll{}, first)
	o.Start(context.Background())

	if added := o.Refresh(context.Background()); len(added) != 0 {
		t.Fatalf("Refresh with no new agents returned %v, want empty", added)
	}

	if err := o.reg.Register(context.Background(), &stubAgent{id: "alpha", name: "alpha"}); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if err := o.reg.Register(context.Background(), &stubAgent{id: "beta", name: "beta"}); err != nil {
		t.Fatalf("register beta: %v", err)
	}

	added := o.Refresh(context.Background())
	if len(added) != 2 {
		t.Fatalf("Refresh returned %v, want 2 new IDs", added)
	}
	seen := map[string]bool{}
	for _, id := range added {
		seen[id] = true
	}
	if !seen["alpha"] || !seen["beta"] {
		t.Fatalf("Refresh returned %v, want both alpha and beta", added)
	}

	// And a follow-up Refresh with no further registrations is a no-op.
	if again := o.Refresh(context.Background()); len(again) != 0 {
		t.Fatalf("second Refresh returned %v, want empty", again)
	}
}

// TestRefresh_BeforeStartIsNoOp encodes the chosen contract: Refresh before
// Start does nothing and returns nil. This keeps the initial subscribe pass
// owned by Start (which holds the sync.Once) and avoids a race where Refresh
// would subscribe the initial agent set out from under Start.
func TestRefresh_BeforeStartIsNoOp(t *testing.T) {
	worker := &stubAgent{
		id:    "worker",
		name:  "worker",
		inbox: make(chan agent.Message, 1),
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)

	added := o.Refresh(context.Background())
	if len(added) != 0 {
		t.Fatalf("Refresh before Start returned %v, want empty", added)
	}

	// And verify no subscription was actually wired: a publish should not
	// reach the worker because no Subscribe call ever happened.
	bus.Publish(context.Background(), agent.NewMessage("user", "worker", agent.RoleUser, "task", "pre-start", nil))
	if _, ok := waitFor(worker.inbox, 100*time.Millisecond); ok {
		t.Fatal("worker received a message before Start (Refresh must not subscribe)")
	}
}

// TestWatch_PicksUpNewAgentsAfterInterval exercises the background watcher.
// We register a new agent shortly after Watch starts and assert it receives
// a published message within a bounded deadline — without bare time.Sleep
// gymnastics for synchronisation.
func TestWatch_PicksUpNewAgentsAfterInterval(t *testing.T) {
	first := &stubAgent{id: "first", name: "first"}
	o, bus, _ := buildOrchestrator(t, allowAll{}, first)
	o.Start(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		o.Watch(ctx, 25*time.Millisecond)
		close(done)
	}()

	late := &stubAgent{
		id:    "late",
		name:  "late",
		inbox: make(chan agent.Message, 1),
	}
	// Register after Watch has had at least one tick to spin up.
	time.Sleep(20 * time.Millisecond)
	if err := o.reg.Register(context.Background(), late); err != nil {
		t.Fatalf("register late: %v", err)
	}

	// Eventually-style poll: keep publishing until the watcher's next tick
	// picks up the new agent, or fail at the bounded deadline. Publishing
	// repeatedly is safe because the bus drops messages with no subscriber
	// and the agent's inbox is buffered to 1 so the FIRST successful
	// delivery is what we read.
	deadline := time.Now().Add(500 * time.Millisecond)
	var got agent.Message
	var ok bool
	for time.Now().Before(deadline) {
		bus.Publish(context.Background(), agent.NewMessage("user", "late", agent.RoleUser, "task", "watched", nil))
		select {
		case got = <-late.inbox:
			ok = true
		case <-time.After(40 * time.Millisecond):
		}
		if ok {
			break
		}
	}
	if !ok {
		t.Fatal("Watch did not subscribe the late agent within the deadline")
	}
	if got.Content != "watched" {
		t.Fatalf("unexpected delivery: %+v", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Watch did not return after ctx cancel")
	}
}

// TestRefresh_IgnoresAgentsRemovedFromRegistry documents a deliberate choice:
// if an agent disappears from the registry between Start and Refresh, its
// subscription is NOT torn down. We use a fresh registry that we can mutate
// directly to simulate the (non-standard) removal.
//
// This codifies the design note in Refresh's doc comment.
func TestRefresh_IgnoresAgentsRemovedFromRegistry(t *testing.T) {
	worker := &stubAgent{
		id:    "worker",
		name:  "worker",
		inbox: make(chan agent.Message, 1),
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)
	o.Start(context.Background())

	// Simulate removal by swapping in a fresh, empty registry. The existing
	// subscription on the bus is unaffected.
	o.reg = registry.NewInMemory()

	added := o.Refresh(context.Background())
	if len(added) != 0 {
		t.Fatalf("Refresh after removal returned %v, want empty", added)
	}

	bus.Publish(context.Background(), agent.NewMessage("user", "worker", agent.RoleUser, "task", "still-subscribed", nil))
	if _, ok := waitFor(worker.inbox, 500*time.Millisecond); !ok {
		t.Fatal("worker subscription was torn down by Refresh; it should remain")
	}
}
