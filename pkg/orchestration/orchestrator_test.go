package orchestration

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/protocol"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

// ---------- test helpers ----------

// stubAgent is a configurable agent.Agent useful for table-style integration
// tests of the orchestrator. Inbound messages are recorded on the inbox channel
// (non-blocking) and, unless a custom handler is set, canned outputs are
// returned verbatim.
type stubAgent struct {
	id      string
	name    string
	caps    []string
	inbox   chan agent.Message
	outputs []agent.Message
	err     error
	calls   int32
	handler func(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error)
}

func (s *stubAgent) ID() string             { return s.id }
func (s *stubAgent) Name() string           { return s.name }
func (s *stubAgent) Capabilities() []string { return s.caps }
func (s *stubAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	atomic.AddInt32(&s.calls, 1)
	if s.handler != nil {
		return s.handler(ctx, msg, env)
	}
	if s.inbox != nil {
		// Non-blocking record — tests that care about a single delivery use a
		// buffered channel sized 1+; tests that exercise multiple deliveries
		// size appropriately.
		select {
		case s.inbox <- msg:
		default:
		}
	}
	return s.outputs, s.err
}

func (s *stubAgent) callCount() int32 { return atomic.LoadInt32(&s.calls) }

// fakeClock returns a fixed time for deterministic Now() assertions.
type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

// allowAll is a Policy that allows every message.
type allowAll struct{}

func (allowAll) Evaluate(_ context.Context, _ protocol.Message) (governance.PolicyResult, error) {
	return governance.PolicyResult{Decision: governance.DecisionAllow, Reason: "allow-all"}, nil
}

// denyByType denies messages whose Type matches the configured value and
// allows everything else. Used to verify the orchestrator honours Deny
// decisions and short-circuits agent invocation.
type denyByType struct{ deny string }

func (d denyByType) Evaluate(_ context.Context, msg protocol.Message) (governance.PolicyResult, error) {
	if msg.Type == d.deny {
		return governance.PolicyResult{Decision: governance.DecisionDeny, Reason: "type denied"}, nil
	}
	return governance.PolicyResult{Decision: governance.DecisionAllow, Reason: "allowed"}, nil
}

// newTestEnv constructs a SimpleEnvironment whose log lines are captured in
// the returned buffer for substring assertions.
func newTestEnv() (*SimpleEnvironment, *bytes.Buffer, *syncBuffer) {
	sb := &syncBuffer{}
	logger := observability.NewLoggerTo(sb)
	return &SimpleEnvironment{Logger: logger}, &bytes.Buffer{}, sb
}

// syncBuffer is a goroutine-safe wrapper around bytes.Buffer because the
// in-memory bus delivers messages from goroutines and the orchestrator's log
// writes can race with the test reader.
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

// waitFor blocks until ch produces a value or the deadline elapses. It returns
// the received value and a boolean indicating whether the receive succeeded.
func waitFor[T any](ch <-chan T, d time.Duration) (T, bool) {
	var zero T
	select {
	case v := <-ch:
		return v, true
	case <-time.After(d):
		return zero, false
	}
}

// buildOrchestrator wires a fresh orchestrator with the supplied agents and
// policy, an InMemoryBus, an InMemoryRegistry, and a log-capturing environment.
// It returns the orchestrator, the bus (for direct Publish), and the syncBuffer
// holding any log output.
func buildOrchestrator(t *testing.T, policy governance.Policy, agents ...agent.Agent) (*Orchestrator, *comm.InMemoryBus, *syncBuffer) {
	t.Helper()
	ctx := context.Background()
	reg := registry.NewInMemory()
	for _, a := range agents {
		if err := reg.Register(ctx, a); err != nil {
			t.Fatalf("register %s: %v", a.ID(), err)
		}
	}
	bus := comm.NewInMemoryBus()
	env, _, logs := newTestEnv()
	o := NewOrchestrator(reg, bus, policy, env)
	return o, bus, logs
}

// ---------- tests ----------

func TestOrchestrator_DispatchesMessageToTargetedAgent(t *testing.T) {
	worker := &stubAgent{
		id:    "worker",
		name:  "worker",
		caps:  []string{"do"},
		inbox: make(chan agent.Message, 1),
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)
	o.Start(context.Background())

	sent := agent.NewMessage("user", "worker", agent.RoleUser, "task", "do the thing", nil)
	bus.Publish(context.Background(), sent)

	got, ok := waitFor(worker.inbox, 500*time.Millisecond)
	if !ok {
		t.Fatalf("worker did not receive message within deadline")
	}
	if got.ID != sent.ID {
		t.Fatalf("got message id %q, want %q", got.ID, sent.ID)
	}
	if got.Type != "task" || got.Content != "do the thing" {
		t.Fatalf("unexpected message contents: %+v", got)
	}
}

func TestOrchestrator_AgentOutputsAreRepublished(t *testing.T) {
	forwarded := agent.NewMessage("first", "second", agent.RoleAgent, "forward", "to second", nil)
	first := &stubAgent{
		id:      "first",
		name:    "first",
		outputs: []agent.Message{forwarded},
	}
	second := &stubAgent{
		id:    "second",
		name:  "second",
		inbox: make(chan agent.Message, 1),
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, first, second)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage("user", "first", agent.RoleUser, "kick", "start chain", nil))

	got, ok := waitFor(second.inbox, 500*time.Millisecond)
	if !ok {
		t.Fatalf("second agent did not receive forwarded message")
	}
	if got.From != "first" || got.To != "second" || got.Content != "to second" {
		t.Fatalf("unexpected forwarded message: %+v", got)
	}
}

func TestOrchestrator_PolicyDenyShortCircuitsAgent(t *testing.T) {
	worker := &stubAgent{
		id:    "worker",
		name:  "worker",
		inbox: make(chan agent.Message, 1),
	}
	o, bus, logs := buildOrchestrator(t, denyByType{deny: "blocked"}, worker)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage("user", "worker", agent.RoleUser, "blocked", "noop", nil))

	if _, ok := waitFor(worker.inbox, 500*time.Millisecond); ok {
		t.Fatalf("worker should not have received a denied message")
	}
	if got := worker.callCount(); got != 0 {
		t.Fatalf("HandleMessage called %d times, want 0", got)
	}
	if !strings.Contains(logs.String(), "denied by policy") {
		t.Fatalf("expected deny log entry, got: %q", logs.String())
	}
}

func TestOrchestrator_PolicyAllowReachesAgent(t *testing.T) {
	worker := &stubAgent{
		id:    "worker",
		name:  "worker",
		inbox: make(chan agent.Message, 1),
	}
	o, bus, _ := buildOrchestrator(t, denyByType{deny: "blocked"}, worker)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage("user", "worker", agent.RoleUser, "ok", "go", nil))

	got, ok := waitFor(worker.inbox, 500*time.Millisecond)
	if !ok {
		t.Fatalf("worker did not receive allowed message")
	}
	if got.Type != "ok" {
		t.Fatalf("unexpected message type: %q", got.Type)
	}
}

func TestOrchestrator_HandlerErrorIsLoggedAndPipelineContinues(t *testing.T) {
	delivered := make(chan agent.Message, 2)
	var calls int32
	worker := &stubAgent{
		id:   "worker",
		name: "worker",
		handler: func(_ context.Context, msg agent.Message, _ agent.Environment) ([]agent.Message, error) {
			n := atomic.AddInt32(&calls, 1)
			delivered <- msg
			if n == 1 {
				return nil, errors.New("synthetic handler failure")
			}
			return nil, nil
		},
	}
	o, bus, logs := buildOrchestrator(t, allowAll{}, worker)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage("user", "worker", agent.RoleUser, "first", "boom", nil))
	if _, ok := waitFor(delivered, 500*time.Millisecond); !ok {
		t.Fatalf("first delivery did not occur")
	}

	// Wait for the error to be logged before sending the next message so the
	// substring assertion is meaningful.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if strings.Contains(logs.String(), "synthetic handler failure") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "synthetic handler failure") {
		t.Fatalf("expected error log to contain failure text, got: %q", logs.String())
	}

	// A second message should still reach the agent — the pump must not die
	// because of a handler error.
	bus.Publish(context.Background(), agent.NewMessage("user", "worker", agent.RoleUser, "second", "still alive", nil))
	got, ok := waitFor(delivered, 500*time.Millisecond)
	if !ok {
		t.Fatalf("second delivery did not occur after handler error")
	}
	if got.Type != "second" {
		t.Fatalf("expected second delivery type 'second', got %q", got.Type)
	}
}

func TestOrchestrator_UnknownAgentIDIsHandledGracefully(t *testing.T) {
	other := &stubAgent{
		id:    "other",
		name:  "other",
		inbox: make(chan agent.Message, 1),
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, other)
	o.Start(context.Background())

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("publish to unknown agent panicked: %v", r)
		}
	}()

	bus.Publish(context.Background(), agent.NewMessage("user", "nonexistent", agent.RoleUser, "task", "void", nil))

	if _, ok := waitFor(other.inbox, 250*time.Millisecond); ok {
		t.Fatalf("message for nonexistent agent should not be delivered to any subscriber")
	}
	if got := other.callCount(); got != 0 {
		t.Fatalf("unrelated agent invoked %d times, want 0", got)
	}
}

func TestOrchestrator_StartIsIdempotent(t *testing.T) {
	// Start is wrapped in sync.Once: a second invocation must not
	// double-subscribe agents. With two Starts and one Publish, the worker
	// should record exactly one HandleMessage call.
	worker := &stubAgent{
		id:    "worker",
		name:  "worker",
		inbox: make(chan agent.Message, 4),
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)
	o.Start(context.Background())
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage("user", "worker", agent.RoleUser, "task", "once", nil))

	// Wait for delivery, then give the bus extra grace to surface a
	// spurious second delivery if double-subscription occurred.
	if _, ok := waitFor(worker.inbox, 500*time.Millisecond); !ok {
		t.Fatal("worker never received the publish")
	}
	time.Sleep(150 * time.Millisecond)

	if got := worker.callCount(); got != 1 {
		t.Fatalf("expected 1 invocation per publish, got %d", got)
	}
}

func TestSimpleEnvironment_NowReturnsClockTime(t *testing.T) {
	fixed := time.Date(2025, 3, 14, 15, 9, 26, 0, time.UTC)
	env := &SimpleEnvironment{Clock: fakeClock{t: fixed}}
	if got := env.Now(); !got.Equal(fixed) {
		t.Fatalf("env.Now() = %v, want %v", got, fixed)
	}
}

func TestSimpleEnvironment_LogfForwardsToLogger(t *testing.T) {
	var buf bytes.Buffer
	env := &SimpleEnvironment{Logger: observability.NewLoggerTo(&buf)}
	env.Logf("hello %s", "world")
	if !strings.Contains(buf.String(), "hello world") {
		t.Fatalf("expected log to contain %q, got %q", "hello world", buf.String())
	}
}
