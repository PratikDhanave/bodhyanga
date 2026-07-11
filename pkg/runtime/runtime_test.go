package runtime

// Tests for the per-agent message loop. handle() is exercised synchronously
// (it is a plain method; the bus's goroutine dispatch is orthogonal), so
// admit / fan-out / error assertions are deterministic. Run()'s async
// Subscribe→handler path and its ctx/stop lifecycle get end-to-end tests over
// a real InMemoryBus. The test doubles are mutex-guarded so the concurrent and
// ctx-cancellation tests stay clean under -race.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/gateway"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// --- test doubles ---------------------------------------------------------

// stubAgent is a controllable, concurrency-safe agent.Agent. out/err drive
// HandleMessage; panicOnType makes it panic for one message type; calls/gotMsg/
// gotEnv record what it was invoked with (read via the accessors).
type stubAgent struct {
	id          string
	out         []agent.Message
	err         error
	panicOnType string

	mu     sync.Mutex
	calls  int
	gotMsg agent.Message
	gotEnv agent.Environment
}

func (a *stubAgent) ID() string             { return a.id }
func (a *stubAgent) Name() string           { return a.id + "-name" }
func (a *stubAgent) Capabilities() []string { return nil }

func (a *stubAgent) HandleMessage(_ context.Context, m agent.Message, env agent.Environment) ([]agent.Message, error) {
	// Panic before taking the lock so a panic never leaves it held.
	if a.panicOnType != "" && m.Type == a.panicOnType {
		panic("stubAgent boom on type " + m.Type)
	}
	a.mu.Lock()
	a.calls++
	a.gotMsg = m
	a.gotEnv = env
	a.mu.Unlock()
	return a.out, a.err
}

func (a *stubAgent) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func (a *stubAgent) lastMsg() agent.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.gotMsg
}

func (a *stubAgent) lastEnv() agent.Environment {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.gotEnv
}

// stubEnv is a minimal agent.Environment. Pointer receiver so tests can assert
// the runtime forwards the *same* Env instance by identity.
type stubEnv struct{}

func (*stubEnv) Now() time.Time      { return time.Unix(0, 0).UTC() }
func (*stubEnv) Logf(string, ...any) {}

// stubPolicy is a controllable governance.Policy for driving Admit branches.
type stubPolicy struct {
	decision governance.Decision
	err      error
}

func (s stubPolicy) Evaluate(_ context.Context, _ protocol.Message) (governance.PolicyResult, error) {
	if s.err != nil {
		return governance.PolicyResult{}, s.err
	}
	return governance.PolicyResult{Decision: s.decision, Reason: "stub-reason"}, nil
}

// captureBus records Subscribe/Publish/unsubscribe. Mutex-guarded so the
// ctx-cancel and concurrent tests are race-clean. unsubCh closes on the first
// unsubscribe so a test can wait for an async teardown.
type captureBus struct {
	mu        sync.Mutex
	subs      map[string]comm.Handler
	published []protocol.Message
	unsubbed  bool
	unsubCh   chan struct{}
}

func (b *captureBus) Subscribe(agentID string, h comm.Handler) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs == nil {
		b.subs = map[string]comm.Handler{}
	}
	if b.unsubCh == nil {
		b.unsubCh = make(chan struct{})
	}
	b.subs[agentID] = h
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.unsubbed {
			b.unsubbed = true
			close(b.unsubCh)
		}
	}
}

func (b *captureBus) Publish(_ context.Context, m protocol.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, m)
}

func (b *captureBus) hasSub(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.subs[id]
	return ok
}

func (b *captureBus) subCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

func (b *captureBus) pub() []protocol.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]protocol.Message(nil), b.published...)
}

func (b *captureBus) wasUnsubbed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.unsubbed
}

func (b *captureBus) waitUnsub(t *testing.T, d time.Duration) {
	t.Helper()
	b.mu.Lock()
	ch := b.unsubCh
	b.mu.Unlock()
	if ch == nil {
		t.Fatal("nothing subscribed to wait on")
	}
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatal("timed out waiting for unsubscribe")
	}
}

const phiMarker = "PHI-SECRET-NARRATIVE"

func rmsg(typ string) protocol.Message {
	return protocol.Message{ID: "m1", From: "user", To: "agent", Type: typ, Content: phiMarker}
}

func countKind(events []audit.Event, k audit.Kind) int {
	n := 0
	for _, e := range events {
		if e.Kind == k {
			n++
		}
	}
	return n
}

// --- Run validation -------------------------------------------------------

func TestRun_ValidatesRequiredFields(t *testing.T) {
	full := func() *Runtime {
		return &Runtime{
			Agent:    &stubAgent{id: "a"},
			Bus:      &captureBus{},
			Pipeline: &gateway.Pipeline{AgentID: "a"},
			Env:      &stubEnv{},
		}
	}
	cases := []struct {
		name   string
		mutate func(*Runtime)
		want   string
	}{
		{"nil agent", func(r *Runtime) { r.Agent = nil }, "Agent"},
		{"nil bus", func(r *Runtime) { r.Bus = nil }, "Bus"},
		{"nil pipeline", func(r *Runtime) { r.Pipeline = nil }, "Pipeline"},
		{"nil env", func(r *Runtime) { r.Env = nil }, "Env"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := full()
			tc.mutate(r)
			stop, err := r.Run(context.Background())
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if stop != nil {
				t.Error("stop must be nil when Run errors")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should name the missing field %q", err, tc.want)
			}
		})
	}
}

// A validation failure must not burn the single-shot guard.
func TestRun_ValidationFailure_DoesNotConsumeGuard(t *testing.T) {
	bus := &captureBus{}
	r := &Runtime{Agent: &stubAgent{id: "a"}, Bus: bus, Pipeline: &gateway.Pipeline{AgentID: "a"}}
	if _, err := r.Run(context.Background()); err == nil {
		t.Fatal("expected error for nil Env")
	}
	r.Env = &stubEnv{} // fix the wiring and retry
	stop, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("corrected retry should start: %v", err)
	}
	defer stop()
	if !bus.hasSub("a") {
		t.Error("corrected retry should subscribe")
	}
}

func TestRun_SubscribesOnAgentIDAndStopUnsubscribes(t *testing.T) {
	bus := &captureBus{}
	r := &Runtime{
		Agent:    &stubAgent{id: "diagnostician"},
		Bus:      bus,
		Pipeline: &gateway.Pipeline{AgentID: "diagnostician"},
		Env:      &stubEnv{},
	}
	stop, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !bus.hasSub("diagnostician") {
		t.Fatal("Run must Subscribe on Agent.ID()")
	}
	if bus.subCount() != 1 {
		t.Errorf("Run must subscribe exactly once, got %d subscriptions", bus.subCount())
	}
	if stop == nil {
		t.Fatal("stop must be non-nil on success")
	}
	stop()
	if !bus.wasUnsubbed() {
		t.Error("stop() must unsubscribe")
	}
}

// Run is single-shot: a second call errors instead of double-subscribing.
func TestRun_SecondCall_Errors(t *testing.T) {
	bus := &captureBus{}
	r := &Runtime{Agent: &stubAgent{id: "a"}, Bus: bus, Pipeline: &gateway.Pipeline{AgentID: "a"}, Env: &stubEnv{}}

	stop, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	defer stop()

	if _, err2 := r.Run(context.Background()); err2 == nil || !strings.Contains(err2.Error(), "already") {
		t.Fatalf("second Run should error, got %v", err2)
	}
	if bus.subCount() != 1 {
		t.Errorf("second Run must not double-subscribe; got %d subscriptions", bus.subCount())
	}
}

// stop() is idempotent — calling it twice must not panic or double-unsubscribe.
func TestRun_Stop_Idempotent(t *testing.T) {
	bus := &captureBus{}
	r := &Runtime{Agent: &stubAgent{id: "a"}, Bus: bus, Pipeline: &gateway.Pipeline{AgentID: "a"}, Env: &stubEnv{}}
	stop, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	stop()
	stop() // must be safe
	if !bus.wasUnsubbed() {
		t.Error("stop() must unsubscribe")
	}
}

// Cancelling the ctx tears the subscription down even without a stop() call.
func TestRun_CtxCancel_Unsubscribes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bus := &captureBus{}
	r := &Runtime{Agent: &stubAgent{id: "a"}, Bus: bus, Pipeline: &gateway.Pipeline{AgentID: "a"}, Env: &stubEnv{}}

	stop, err := r.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer stop()

	if bus.wasUnsubbed() {
		t.Fatal("must not unsubscribe before ctx is cancelled")
	}
	cancel()
	bus.waitUnsub(t, 2*time.Second)
}

// --- handle: admission + fan-out ------------------------------------------

func TestHandle_Admit_FanOutInOrder(t *testing.T) {
	bus := &captureBus{}
	rec := audit.NewInMemory()
	a := &stubAgent{id: "agent", out: []agent.Message{{ID: "o1", To: "x"}, {ID: "o2", To: "y"}}}
	r := &Runtime{Agent: a, Bus: bus, Pipeline: &gateway.Pipeline{AgentID: "agent", Recorder: rec}, Env: &stubEnv{}}

	r.handle(context.Background(), rmsg("case_state"))

	if a.callCount() != 1 {
		t.Fatalf("agent invoked %d times, want 1", a.callCount())
	}
	pub := bus.pub()
	if len(pub) != 2 {
		t.Fatalf("published %d messages, want 2", len(pub))
	}
	if pub[0].ID != "o1" || pub[1].ID != "o2" {
		t.Errorf("fan-out order wrong: %q then %q", pub[0].ID, pub[1].ID)
	}
	if got := countKind(rec.List(), audit.KindMessage); got != 1 {
		t.Errorf("admit should record 1 message event, got %d", got)
	}
}

// The agent must receive the inbound message and the runtime's Env unmodified.
func TestHandle_PassesMessageAndEnvThrough(t *testing.T) {
	env := &stubEnv{}
	a := &stubAgent{id: "agent"}
	r := &Runtime{Agent: a, Bus: &captureBus{}, Pipeline: &gateway.Pipeline{AgentID: "agent"}, Env: env}

	in := rmsg("case_state")
	r.handle(context.Background(), in)

	got := a.lastMsg()
	if got.ID != in.ID || got.Type != in.Type || got.From != in.From || got.Content != in.Content {
		t.Errorf("agent received a mutated message:\n got  %+v\n want %+v", got, in)
	}
	if a.lastEnv() != agent.Environment(env) {
		t.Error("runtime must forward its own Env instance to the agent")
	}
}

func TestHandle_NoOutput_NoPublish(t *testing.T) {
	bus := &captureBus{}
	a := &stubAgent{id: "agent"} // nil out, nil err (terminal handler)
	r := &Runtime{Agent: a, Bus: bus, Pipeline: &gateway.Pipeline{AgentID: "agent"}, Env: &stubEnv{}}

	r.handle(context.Background(), rmsg("diagnosis"))

	if a.callCount() != 1 {
		t.Fatalf("agent invoked %d times, want 1", a.callCount())
	}
	if len(bus.pub()) != 0 {
		t.Fatalf("terminal handler must publish nothing, got %d", len(bus.pub()))
	}
}

// --- handle: deny short-circuits before the agent -------------------------

func TestHandle_Deny_BlocksAgentAndFanOut(t *testing.T) {
	bus := &captureBus{}
	rec := audit.NewInMemory()
	a := &stubAgent{id: "agent", out: []agent.Message{{ID: "o1"}}}
	p := &gateway.Pipeline{AgentID: "agent", Policy: stubPolicy{decision: governance.DecisionDeny}, Recorder: rec}
	r := &Runtime{Agent: a, Bus: bus, Pipeline: p, Env: &stubEnv{}}

	r.handle(context.Background(), rmsg("case_state"))

	if a.callCount() != 0 {
		t.Fatal("agent must NOT be invoked when admission denies")
	}
	if len(bus.pub()) != 0 {
		t.Fatal("nothing may be published when admission denies")
	}
	if got := countKind(rec.List(), audit.KindPolicyDecision); got != 1 {
		t.Errorf("deny should record 1 policy_decision, got %d", got)
	}
	if got := countKind(rec.List(), audit.KindMessage); got != 0 {
		t.Errorf("deny must not record a message/invocation event, got %d", got)
	}
}

func TestHandle_PolicyError_BlocksAgent(t *testing.T) {
	bus := &captureBus{}
	a := &stubAgent{id: "agent", out: []agent.Message{{ID: "o1"}}}
	p := &gateway.Pipeline{AgentID: "agent", Policy: stubPolicy{err: errors.New("policy exploded")}}
	r := &Runtime{Agent: a, Bus: bus, Pipeline: p, Env: &stubEnv{}}

	r.handle(context.Background(), rmsg("case_state"))

	if a.callCount() != 0 {
		t.Fatal("policy error must fail closed — agent not invoked")
	}
	if len(bus.pub()) != 0 {
		t.Fatal("policy error must publish nothing")
	}
}

// --- handle: agent error audits and drops (fidelity) ----------------------

func TestHandle_AgentError_AuditsAndDrops(t *testing.T) {
	bus := &captureBus{}
	rec := audit.NewInMemory()
	a := &stubAgent{id: "agent", out: []agent.Message{{ID: "o1"}}, err: errors.New("boom")}
	r := &Runtime{Agent: a, Bus: bus, Pipeline: &gateway.Pipeline{AgentID: "agent", Recorder: rec}, Env: &stubEnv{}}

	r.handle(context.Background(), rmsg("case_state"))

	if len(bus.pub()) != 0 {
		t.Fatal("on agent error the outputs must be dropped (fidelity with the monolith)")
	}
	var agentErrs int
	for _, e := range rec.List() {
		if e.Kind == audit.KindAgentError {
			agentErrs++
			if e.ErrorText != "boom" {
				t.Errorf("ErrorText = %q, want %q", e.ErrorText, "boom")
			}
			if e.AgentID != "agent" {
				t.Errorf("agent_error AgentID = %q, want %q", e.AgentID, "agent")
			}
		}
	}
	if agentErrs != 1 {
		t.Fatalf("want exactly 1 agent_error event, got %d", agentErrs)
	}
	if got := countKind(rec.List(), audit.KindMessage); got != 1 {
		t.Errorf("want 1 message event before the error, got %d", got)
	}
}

// Runtime must NOT recover a handler panic — the bus's safeHandle does, and on
// the durable bus that recovery drives Ack vs. Nak/redelivery. A stray recover
// here would silently Ack (and lose) a message that should be redelivered.
func TestHandle_PanicPropagates_RuntimeAddsNoRecover(t *testing.T) {
	a := &stubAgent{id: "agent", panicOnType: "case_state"}
	r := &Runtime{Agent: a, Bus: &captureBus{}, Pipeline: &gateway.Pipeline{AgentID: "agent"}, Env: &stubEnv{}}

	defer func() {
		if got := recover(); got == nil {
			t.Fatal("handle must let a handler panic propagate; Runtime must not recover it")
		}
	}()
	r.handle(context.Background(), rmsg("case_state"))
	t.Fatal("expected the handler panic to propagate out of handle()")
}

// --- PHI safety: msg.Content never reaches the audit log ------------------

func TestHandle_NeverAuditsContent(t *testing.T) {
	rec := audit.NewInMemory()
	a := &stubAgent{id: "agent", err: errors.New("boom")}
	p := &gateway.Pipeline{
		AgentID:       "agent",
		Policy:        stubPolicy{decision: governance.DecisionAllow},
		Recorder:      rec,
		ClinicalTypes: gateway.DefaultClinicalTypes(),
	}
	r := &Runtime{Agent: a, Bus: &captureBus{}, Pipeline: p, Env: &stubEnv{}}

	// A clinical type exercises the policy-decision, message, and agent-error
	// audit paths at once — every event kind the runtime can produce.
	r.handle(context.Background(), rmsg("diagnosis"))

	events := rec.List()
	if len(events) == 0 {
		t.Fatal("expected audit events to inspect")
	}
	for _, e := range events {
		blob, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		if strings.Contains(string(blob), phiMarker) {
			t.Fatalf("PHI leaked into audit event %s: %s", e.Kind, blob)
		}
	}
}

// --- end-to-end over a real InMemoryBus -----------------------------------

func TestRun_EndToEnd_RealBus(t *testing.T) {
	bus := comm.NewInMemoryBus()
	got := make(chan protocol.Message, 1)

	a := &stubAgent{id: "agent", out: []agent.Message{{ID: "o1", To: "downstream", Type: "result"}}}
	r := &Runtime{Agent: a, Bus: bus, Pipeline: &gateway.Pipeline{AgentID: "agent"}, Env: &stubEnv{}}

	stop, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer stop()

	bus.Subscribe("downstream", func(_ context.Context, m protocol.Message) { got <- m })
	bus.Publish(context.Background(), protocol.Message{ID: "in", To: "agent", Type: "case_state"})

	select {
	case m := <-got:
		if m.ID != "o1" {
			t.Errorf("downstream received %q, want o1", m.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the runtime to fan out the agent's output")
	}
}

// Concurrent delivery: the bus invokes handle() on its own goroutine per
// message, so N overlapping messages must each be handled and fanned out. Run
// under -race, this pins the runtime as safe for concurrent delivery.
func TestRun_ConcurrentDelivery_RealBus(t *testing.T) {
	const n = 20
	bus := comm.NewInMemoryBus()
	got := make(chan protocol.Message, n)

	a := &stubAgent{id: "agent", out: []agent.Message{{ID: "o", To: "downstream", Type: "result"}}}
	r := &Runtime{Agent: a, Bus: bus, Pipeline: &gateway.Pipeline{AgentID: "agent"}, Env: &stubEnv{}}
	stop, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer stop()

	bus.Subscribe("downstream", func(_ context.Context, m protocol.Message) { got <- m })

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish(context.Background(), protocol.Message{To: "agent", Type: "case_state"})
		}()
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		select {
		case <-got:
		case <-time.After(3 * time.Second):
			t.Fatalf("received only %d of %d fan-out outputs", i, n)
		}
	}
	if a.callCount() != n {
		t.Errorf("agent handled %d messages, want %d", a.callCount(), n)
	}
}
