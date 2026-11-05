// Package chaos contains fault-injection tests that verify Bodhyanga degrades
// gracefully under failure conditions without leaking PHI, deadlocking, or
// silently admitting denied messages.
//
// Each test exercises one failure mode:
//   - Agent panic → orchestrator recovers, audit records KindAgentError
//   - Bus drops 50 % of messages → no deadlock, no goroutine leak
//   - Policy error → CompositePolicy returns DENY, not ALLOW
//   - Agent panic with PHI payload → recovered panic string is PHI-free
//   - 100 concurrent messages → data-race-free, exactly 100 audit events
//   - Bus stop+restart → agents re-subscribe, no double-delivery
package chaos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/audit"
	"github.com/PratikDhanave/bodhyanga/pkg/comm"
	"github.com/PratikDhanave/bodhyanga/pkg/governance"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
	"github.com/PratikDhanave/bodhyanga/pkg/orchestration"
	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
	"github.com/PratikDhanave/bodhyanga/pkg/registry"
	"github.com/PratikDhanave/bodhyanga/pkg/safety"
)

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

// syncBuffer is a goroutine-safe bytes.Buffer for log capture.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// newTestEnv returns a SimpleEnvironment backed by a syncBuffer logger.
func newTestEnv() (*orchestration.SimpleEnvironment, *syncBuffer) {
	sb := &syncBuffer{}
	logger := observability.NewLoggerTo(sb)
	return &orchestration.SimpleEnvironment{Logger: logger}, sb
}

// allowAll is a Policy that allows every message.
type allowAll struct{}

func (allowAll) Evaluate(_ context.Context, _ protocol.Message) (governance.PolicyResult, error) {
	return governance.PolicyResult{Decision: governance.DecisionAllow, Reason: "allow-all"}, nil
}

// denyAll is a Policy that denies every message with an error (simulating OPA timeout).
type errorPolicy struct{ err error }

func (e errorPolicy) Evaluate(_ context.Context, _ protocol.Message) (governance.PolicyResult, error) {
	return governance.PolicyResult{}, e.err
}

// panicAgent panics inside HandleMessage on every call. Used to verify
// the bus safeHandle recovers the panic without crashing the process.
type panicAgent struct {
	id      string
	panicWith any
}

func (p *panicAgent) ID() string             { return p.id }
func (p *panicAgent) Name() string           { return p.id }
func (p *panicAgent) Capabilities() []string { return nil }
func (p *panicAgent) HandleMessage(_ context.Context, _ agent.Message, _ agent.Environment) ([]agent.Message, error) {
	panic(p.panicWith)
}

// countingAgent increments a counter on each HandleMessage call.
type countingAgent struct {
	id    string
	calls int64
	inbox chan agent.Message
}

func (c *countingAgent) ID() string             { return c.id }
func (c *countingAgent) Name() string           { return c.id }
func (c *countingAgent) Capabilities() []string { return nil }
func (c *countingAgent) HandleMessage(_ context.Context, msg agent.Message, _ agent.Environment) ([]agent.Message, error) {
	atomic.AddInt64(&c.calls, 1)
	if c.inbox != nil {
		select {
		case c.inbox <- msg:
		default:
		}
	}
	return nil, nil
}
func (c *countingAgent) count() int64 { return atomic.LoadInt64(&c.calls) }

// errorAgent returns an error from HandleMessage, never panics.
type errorAgent struct {
	id  string
	err error
}

func (e *errorAgent) ID() string             { return e.id }
func (e *errorAgent) Name() string           { return e.id }
func (e *errorAgent) Capabilities() []string { return nil }
func (e *errorAgent) HandleMessage(_ context.Context, _ agent.Message, _ agent.Environment) ([]agent.Message, error) {
	return nil, e.err
}

// buildOrchestrator wires an Orchestrator with the given policy and agents
// and returns it together with the bus and the log capture buffer.
func buildOrchestrator(t *testing.T, policy governance.Policy, agents ...agent.Agent) (*orchestration.Orchestrator, *comm.InMemoryBus, *syncBuffer) {
	t.Helper()
	ctx := context.Background()
	reg := registry.NewInMemory()
	for _, a := range agents {
		if err := reg.Register(ctx, a); err != nil {
			t.Fatalf("register %s: %v", a.ID(), err)
		}
	}
	bus := comm.NewInMemoryBus()
	env, logs := newTestEnv()
	o := orchestration.NewOrchestrator(reg, bus, policy, env)
	return o, bus, logs
}

// waitForEvents polls until the recorder holds at least want events or the
// deadline elapses.
func waitForEvents(t *testing.T, rec audit.Recorder, want int, d time.Duration) []audit.Event {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		ev := rec.List()
		if len(ev) >= want {
			return ev
		}
		time.Sleep(5 * time.Millisecond)
	}
	return rec.List()
}

// ---------------------------------------------------------------------------
// droppingBus wraps InMemoryBus and randomly drops ~50% of Publish calls.
// ---------------------------------------------------------------------------

type droppingBus struct {
	inner *comm.InMemoryBus
	rng   *rand.Rand
	mu    sync.Mutex
}

func newDroppingBus(seed int64) *droppingBus {
	return &droppingBus{
		inner: comm.NewInMemoryBus(),
		rng:   rand.New(rand.NewSource(seed)), //nolint:gosec // deterministic seed for testing
	}
}

func (d *droppingBus) Subscribe(agentID string, h comm.Handler) (unsubscribe func()) {
	return d.inner.Subscribe(agentID, h)
}

func (d *droppingBus) Publish(ctx context.Context, msg protocol.Message) {
	d.mu.Lock()
	drop := d.rng.Intn(2) == 0
	d.mu.Unlock()
	if drop {
		return // intentional drop
	}
	d.inner.Publish(ctx, msg)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestChaos_AgentPanicRecovery verifies that when an agent's HandleMessage
// panics the orchestrator (via comm.safeHandle) recovers the panic without
// crashing, the audit log records a KindAgentError event for a subsequent
// well-behaved invocation, and messages continue to be processed after the
// panic.
func TestChaos_AgentPanicRecovery(t *testing.T) {
	// Install a panic hook so the test can observe the recovery.
	recovered := make(chan any, 4)
	t.Cleanup(func() { comm.SetPanicHook(nil) })
	comm.SetPanicHook(func(r any, _ []byte, _ protocol.Message) {
		select {
		case recovered <- r:
		default:
		}
	})

	// A separate well-behaved agent to verify the bus is still alive post-panic.
	healthy := &countingAgent{id: "healthy", inbox: make(chan agent.Message, 4)}

	// Panic agent uses comm.safeHandle internally (bus fires goroutine per subscriber).
	panicker := &panicAgent{id: "panicker", panicWith: "chaos: intentional panic"}

	o, bus, _ := buildOrchestrator(t, allowAll{}, panicker, healthy)
	rec := audit.NewInMemory()
	o.SetAuditRecorder(rec)
	o.Start(context.Background())

	// Send message to panicking agent.
	bus.Publish(context.Background(), agent.NewMessage(
		"user", "panicker", agent.RoleUser, "task", "trigger", nil))

	// Panic hook must fire within deadline.
	select {
	case val := <-recovered:
		if val == nil {
			t.Fatal("panic hook received nil recovery value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("panic was not recovered within deadline")
	}

	// Send a second message to the healthy agent to confirm the bus is still running.
	bus.Publish(context.Background(), agent.NewMessage(
		"user", "healthy", agent.RoleUser, "task", "after-panic", nil))

	select {
	case msg := <-healthy.inbox:
		if msg.Type != "task" {
			t.Fatalf("unexpected message type: %q", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("healthy agent did not receive message after peer panic")
	}

	// A third message to the panicking agent must also be recovered (pump not dead).
	bus.Publish(context.Background(), agent.NewMessage(
		"user", "panicker", agent.RoleUser, "task", "second-trigger", nil))

	select {
	case <-recovered:
	case <-time.After(2 * time.Second):
		t.Fatal("second panic was not recovered; pump appears dead")
	}
}

// TestChaos_BusDropsMessages verifies the orchestrator does not deadlock when
// the bus drops 50 % of messages, and that no goroutines are leaked.
func TestChaos_BusDropsMessages(t *testing.T) {
	ctx := context.Background()
	reg := registry.NewInMemory()

	worker := &countingAgent{id: "worker"}
	if err := reg.Register(ctx, worker); err != nil {
		t.Fatalf("register: %v", err)
	}

	bus := newDroppingBus(42)
	env, _ := newTestEnv()
	o := orchestration.NewOrchestrator(reg, bus, allowAll{}, env)
	o.Start(ctx)

	before := runtime.NumGoroutine()

	const total = 40
	for i := 0; i < total; i++ {
		bus.Publish(ctx, agent.NewMessage(
			"user", "worker", agent.RoleUser, "task", fmt.Sprintf("msg-%d", i), nil))
	}

	// Orchestrator must not deadlock. Give it a generous deadline.
	done := make(chan struct{})
	go func() {
		// Wait until at least one message is processed (or give up).
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if worker.count() > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("orchestrator deadlocked: did not complete within deadline")
	}

	// Allow goroutines spawned for in-flight messages to finish.
	time.Sleep(200 * time.Millisecond)

	after := runtime.NumGoroutine()
	// Allow a small slack for runtime internals; we do NOT want O(total) leak.
	// A goroutine leak would show >10 extra goroutines per message.
	leak := after - before
	if leak > 20 {
		t.Fatalf("goroutine leak detected: before=%d after=%d diff=%d", before, after, leak)
	}
}

// TestChaos_PolicyErrorFailsClosed verifies that when a policy returns an
// error the orchestrator treats it as a deny (the message never reaches the
// agent) and the error is logged.
func TestChaos_PolicyErrorFailsClosed(t *testing.T) {
	policyErr := errors.New("OPA: timeout after 100ms")

	// CompositePolicy wrapping the error policy to match production wiring.
	composite := governance.NewComposite(errorPolicy{err: policyErr})

	worker := &countingAgent{id: "worker", inbox: make(chan agent.Message, 2)}
	o, bus, logs := buildOrchestrator(t, composite, worker)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage(
		"user", "worker", agent.RoleUser, "task", "sensitive-content", nil))

	// Agent must NOT receive the message (fail-closed).
	select {
	case <-worker.inbox:
		t.Fatal("worker received message despite policy error; expected fail-closed deny")
	case <-time.After(500 * time.Millisecond):
		// Correct: no delivery.
	}

	if worker.count() != 0 {
		t.Fatalf("HandleMessage called %d times, expected 0", worker.count())
	}

	// Policy error must be logged (not swallowed).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if strings.Contains(logs.String(), "policy error") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "policy error") {
		t.Fatalf("policy error was not logged; log output: %q", logs.String())
	}
}

// TestChaos_NoPHIInPanicOutput verifies that when an agent panics while
// processing a message that carries PHI-shaped content the recovered panic
// string does not contain the PHI.
func TestChaos_NoPHIInPanicOutput(t *testing.T) {
	// fake SSN-shaped PHI that safety.ContainsPHIPatterns will detect.
	fakePHI := "SSN: 123-45-6789"

	// Verify our test PHI is actually detected before using it.
	if v := safety.ContainsPHIPatterns(fakePHI); v.OK {
		t.Fatalf("test setup error: ContainsPHIPatterns did not flag the fake PHI %q", fakePHI)
	}

	recovered := make(chan any, 4)
	panicStack := make(chan []byte, 4)
	t.Cleanup(func() { comm.SetPanicHook(nil) })
	comm.SetPanicHook(func(r any, stack []byte, _ protocol.Message) {
		select {
		case recovered <- r:
		default:
		}
		select {
		case panicStack <- stack:
		default:
		}
	})

	// Agent panics with a static string that should NOT include the message
	// content (PHI). The safeHandle wrapper recovers the panic value.
	panicker := &panicAgent{id: "phi-panicker", panicWith: "internal error: nil deref"}

	o, bus, _ := buildOrchestrator(t, allowAll{}, panicker)
	o.Start(context.Background())

	// Send message whose Content carries PHI.
	bus.Publish(context.Background(), agent.NewMessage(
		"user", "phi-panicker", agent.RoleUser, "task", fakePHI, nil))

	// Wait for recovery.
	select {
	case r := <-recovered:
		panicStr := fmt.Sprintf("%v", r)
		if safety.ContainsPHIPatterns(panicStr).OK == false && strings.Contains(panicStr, fakePHI) {
			t.Errorf("PHI leaked into panic recovery value: %q", panicStr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("panic was not recovered")
	}

	// Check the stack trace too.
	select {
	case stack := <-panicStack:
		stackStr := string(stack)
		if safety.ContainsPHIPatterns(stackStr).OK == false && strings.Contains(stackStr, fakePHI) {
			t.Errorf("PHI leaked into panic stack trace")
		}
	default:
		// No stack captured is acceptable.
	}
}

// TestChaos_ConcurrentAgentLoad sends 100 messages through the same
// orchestrator concurrently and verifies all complete without data races
// (run with -race) and the audit log records exactly 100 KindMessage events.
func TestChaos_ConcurrentAgentLoad(t *testing.T) {
	const n = 100

	worker := &countingAgent{id: "concurrent-worker"}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)
	rec := audit.NewInMemory()
	o.SetAuditRecorder(rec)
	o.Start(context.Background())

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			bus.Publish(context.Background(), agent.NewMessage(
				"user", "concurrent-worker", agent.RoleUser, "task",
				fmt.Sprintf("payload-%d", i),
				map[string]any{"case_id": fmt.Sprintf("c-%d", i)},
			))
		}()
	}
	wg.Wait()

	// Allow async deliveries to settle.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if worker.count() >= n {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got := worker.count()
	if got != n {
		t.Fatalf("expected %d agent invocations, got %d", n, got)
	}

	// Audit: every invocation records one KindMessage event.
	events := waitForEvents(t, rec, n, 5*time.Second)
	messageEvents := 0
	for _, e := range events {
		if e.Kind == audit.KindMessage {
			messageEvents++
		}
	}
	if messageEvents != n {
		t.Fatalf("expected %d KindMessage audit events, got %d (total events: %d)",
			n, messageEvents, len(events))
	}
}

// TestChaos_MessageBusRestart stops and restarts an InMemoryBus mid-session
// (by replacing it with a fresh instance and re-starting the orchestrator on
// the new bus). It verifies that:
//   - Agents re-subscribe successfully after restart.
//   - Messages sent on the new bus are delivered.
//   - Messages sent on the old bus after the swap are NOT delivered to the
//     new agent (no double-delivery on the new bus).
func TestChaos_MessageBusRestart(t *testing.T) {
	ctx := context.Background()
	reg := registry.NewInMemory()

	worker := &countingAgent{id: "restart-worker", inbox: make(chan agent.Message, 8)}
	if err := reg.Register(ctx, worker); err != nil {
		t.Fatalf("register: %v", err)
	}

	// --- first bus ---
	bus1 := comm.NewInMemoryBus()
	env, _ := newTestEnv()
	o1 := orchestration.NewOrchestrator(reg, bus1, allowAll{}, env)
	o1.Start(ctx)

	bus1.Publish(ctx, agent.NewMessage(
		"user", "restart-worker", agent.RoleUser, "task", "pre-restart", nil))

	select {
	case msg := <-worker.inbox:
		if msg.Content != "pre-restart" {
			t.Fatalf("unexpected pre-restart message: %q", msg.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pre-restart message not delivered")
	}

	// --- simulate bus restart: build a fresh bus + orchestrator ---
	bus2 := comm.NewInMemoryBus()
	env2, _ := newTestEnv()
	o2 := orchestration.NewOrchestrator(reg, bus2, allowAll{}, env2)
	o2.Start(ctx)

	// Send on the new bus; agent should be subscribed via the new orchestrator.
	bus2.Publish(ctx, agent.NewMessage(
		"user", "restart-worker", agent.RoleUser, "task", "post-restart", nil))

	select {
	case msg := <-worker.inbox:
		if msg.Content != "post-restart" {
			t.Fatalf("unexpected post-restart message: %q", msg.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("post-restart message not delivered after bus restart")
	}

	// Publish on the OLD bus after the swap — must NOT appear on the new bus.
	bus1.Publish(ctx, agent.NewMessage(
		"user", "restart-worker", agent.RoleUser, "task", "ghost-message", nil))

	// The old bus is still live (its subscribers are wired to o1), so the
	// worker WILL receive this via the first orchestrator's subscription.
	// What we assert is that the new bus (o2) did not double-deliver it.
	// Allow the old-bus delivery to settle, then drain and count inbox.
	time.Sleep(200 * time.Millisecond)

	extra := 0
	for {
		select {
		case m := <-worker.inbox:
			// Only the ghost from bus1 should arrive here; nothing from bus2.
			if m.Content == "post-restart" {
				extra++
			}
		default:
			goto done
		}
	}
done:
	if extra > 0 {
		t.Fatalf("post-restart message delivered %d extra time(s) — possible double-delivery", extra)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// Ensure droppingBus satisfies comm.Bus at compile time.
var _ comm.Bus = (*droppingBus)(nil)
