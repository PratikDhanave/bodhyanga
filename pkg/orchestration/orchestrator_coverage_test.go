package orchestration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/audit"
	"github.com/PratikDhanave/bodhyanga/pkg/comm"
	"github.com/PratikDhanave/bodhyanga/pkg/eval"
	"github.com/PratikDhanave/bodhyanga/pkg/governance"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
)

// ---- recordCaseEval branches ----

func TestRecordCaseEval_NilEvalStore(t *testing.T) {
	o := &Orchestrator{
		env: &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})},
	}
	o.recordCaseEval(agent.Message{Content: "test"})
}

func TestRecordCaseEval_DecodeError(t *testing.T) {
	store := &memEvalStore{}
	o := &Orchestrator{
		evalStore: store,
		env:       &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})},
	}
	o.recordCaseEval(agent.Message{Content: "not valid json"})
	if len(store.records) != 0 {
		t.Error("expected no records for decode error")
	}
}

func TestRecordCaseEval_WithIntSteps(t *testing.T) {
	store := &memEvalStore{}
	o := &Orchestrator{
		evalStore: store,
		env:       &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})},
	}
	state := medical.CaseState{CaseID: "case-1", GoldDiagnosis: "flu"}
	msg := agent.Message{
		Content:  medical.EncodeState(state),
		Metadata: map[string]any{"message_steps": 5},
	}
	o.recordCaseEval(msg)
	if len(store.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(store.records))
	}
}

func TestRecordCaseEval_WithFloat64Steps(t *testing.T) {
	store := &memEvalStore{}
	o := &Orchestrator{
		evalStore: store,
		env:       &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})},
	}
	state := medical.CaseState{CaseID: "case-2"}
	msg := agent.Message{
		Content:  medical.EncodeState(state),
		Metadata: map[string]any{"message_steps": float64(3)},
	}
	o.recordCaseEval(msg)
	if len(store.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(store.records))
	}
}

func TestRecordCaseEval_NilMetadata(t *testing.T) {
	store := &memEvalStore{}
	o := &Orchestrator{
		evalStore: store,
		env:       &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})},
	}
	state := medical.CaseState{CaseID: "case-3"}
	msg := agent.Message{
		Content: medical.EncodeState(state),
	}
	o.recordCaseEval(msg)
	if len(store.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(store.records))
	}
}

func TestRecordCaseEval_SaveError(t *testing.T) {
	store := &errEvalStore{}
	o := &Orchestrator{
		evalStore: store,
		env:       &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})},
	}
	state := medical.CaseState{CaseID: "case-4"}
	msg := agent.Message{
		Content: medical.EncodeState(state),
	}
	o.recordCaseEval(msg)
}

// ---- SimpleEnvironment.Now nil clock ----

func TestSimpleEnvironment_NowNilClock(t *testing.T) {
	env := &SimpleEnvironment{}
	now := env.Now()
	if now.IsZero() {
		t.Error("Now() should return non-zero time")
	}
	if now.Location() != time.UTC {
		t.Errorf("Now() should be UTC, got %v", now.Location())
	}
}

func TestSimpleEnvironment_NowWithClock(t *testing.T) {
	fixed := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	env := &SimpleEnvironment{Clock: fixedClock{t: fixed}}
	got := env.Now()
	if !got.Equal(fixed) {
		t.Errorf("Now() = %v, want %v", got, fixed)
	}
}

// ---- SimpleEnvironment.Logf nil logger ----

func TestSimpleEnvironment_LogfNilLogger(t *testing.T) {
	env := &SimpleEnvironment{}
	env.Logf("test message %d", 42)
}

func TestSimpleEnvironment_LogfWithLogger(t *testing.T) {
	ml := &memLogger{}
	env := &SimpleEnvironment{Logger: ml}
	env.Logf("hello %s", "world")
	if ml.last == "" {
		t.Error("expected logger to be called")
	}
}

// ---- Start with governance deny and policy error ----

func TestStart_PolicyDeny(t *testing.T) {
	bus := comm.NewInMemoryBus()
	reg := newMockRegistry([]agent.Agent{&mockAgent{id: "test_agent"}})
	denyPolicy := &mockPolicy{decision: governance.DecisionDeny, reason: "denied by test"}
	recorder := &memAuditRecorder{}
	env := &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})}

	o := NewOrchestrator(reg, bus, denyPolicy, env)
	o.SetAuditRecorder(recorder)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.Message{
		ID:   "msg1",
		To:   "test_agent",
		Type: "presentation",
	})

	time.Sleep(100 * time.Millisecond)

	if len(recorder.List()) == 0 {
		t.Error("expected at least one audit event for deny")
	}
}

func TestStart_PolicyError(t *testing.T) {
	bus := comm.NewInMemoryBus()
	reg := newMockRegistry([]agent.Agent{&mockAgent{id: "test_agent"}})
	errPolicy := &mockPolicy{err: fmt.Errorf("policy error")}
	env := &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})}

	o := NewOrchestrator(reg, bus, errPolicy, env)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.Message{
		ID:   "msg1",
		To:   "test_agent",
		Type: "presentation",
	})

	time.Sleep(100 * time.Millisecond)
}

// ---- Start with agent error ----

func TestStart_AgentError(t *testing.T) {
	bus := comm.NewInMemoryBus()
	agentErr := &mockAgent{id: "err_agent", err: fmt.Errorf("agent broke")}
	reg := newMockRegistry([]agent.Agent{agentErr})
	recorder := &memAuditRecorder{}
	env := &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})}

	o := NewOrchestrator(reg, bus, nil, env)
	o.SetAuditRecorder(recorder)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.Message{
		ID:   "msg1",
		To:   "err_agent",
		Type: "presentation",
	})

	time.Sleep(100 * time.Millisecond)

	hasErr := false
	for _, e := range recorder.List() {
		if e.Kind == audit.KindAgentError {
			hasErr = true
		}
	}
	if !hasErr {
		t.Error("expected KindAgentError audit event")
	}
}

// ---- recordAgentError nil recorder ----

func TestRecordAgentError_NilRecorder(t *testing.T) {
	o := &Orchestrator{
		env: &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})},
	}
	o.recordAgentError("agent1", agent.Message{}, fmt.Errorf("test"))
}

// ---- recordAudit error path ----

func TestRecordAudit_RecorderError(t *testing.T) {
	o := &Orchestrator{
		recorder: &errAuditRecorder{},
		env:      &SimpleEnvironment{Logger: observability.NewLoggerTo(nilWriter{})},
	}
	o.recordAudit(audit.Event{Kind: audit.KindMessage, MessageID: "msg1"})
}

// ---- helpers ----

type nilWriter struct{}

func (nilWriter) Write(p []byte) (int, error) { return len(p), nil }

type memEvalStore struct {
	records []eval.InteractionRecord
}

func (s *memEvalStore) Save(r eval.InteractionRecord) error {
	s.records = append(s.records, r)
	return nil
}
func (s *memEvalStore) List() []eval.InteractionRecord { return s.records }

type errEvalStore struct{}

func (s *errEvalStore) Save(_ eval.InteractionRecord) error { return fmt.Errorf("save failed") }
func (s *errEvalStore) List() []eval.InteractionRecord      { return nil }

type fixedClock struct {
	t time.Time
}

func (f fixedClock) Now() time.Time { return f.t }

type memLogger struct {
	last string
}

func (m *memLogger) Printf(format string, v ...any) {
	m.last = fmt.Sprintf(format, v...)
}

type mockAgent struct {
	id  string
	err error
	out []agent.Message
}

func (m *mockAgent) ID() string           { return m.id }
func (m *mockAgent) Name() string         { return m.id }
func (m *mockAgent) Capabilities() []string { return nil }
func (m *mockAgent) HandleMessage(_ context.Context, msg agent.Message, _ agent.Environment) ([]agent.Message, error) {
	return m.out, m.err
}

type mockPolicy struct {
	decision governance.Decision
	reason   string
	err      error
}

func (m *mockPolicy) Evaluate(_ context.Context, _ agent.Message) (governance.PolicyResult, error) {
	if m.err != nil {
		return governance.PolicyResult{}, m.err
	}
	return governance.PolicyResult{Decision: m.decision, Reason: m.reason}, nil
}

type mockRegistry struct {
	agents []agent.Agent
}

func newMockRegistry(agents []agent.Agent) *mockRegistry {
	return &mockRegistry{agents: agents}
}

func (r *mockRegistry) Register(_ context.Context, _ agent.Agent) error { return nil }
func (r *mockRegistry) Get(_ context.Context, id string) (agent.Agent, error) {
	for _, a := range r.agents {
		if a.ID() == id {
			return a, nil
		}
	}
	return nil, fmt.Errorf("not found: %s", id)
}
func (r *mockRegistry) List(_ context.Context) []agent.Agent { return r.agents }
func (r *mockRegistry) FindByCapability(_ context.Context, _ string) []agent.Agent { return nil }

type memAuditRecorder struct {
	mu     sync.Mutex // Start fans out one goroutine per message; Record is called concurrently
	events []audit.Event
}

func (m *memAuditRecorder) Record(e audit.Event) error {
	m.mu.Lock()
	m.events = append(m.events, e)
	m.mu.Unlock()
	return nil
}
func (m *memAuditRecorder) List() []audit.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]audit.Event(nil), m.events...)
}

type errAuditRecorder struct{}

func (e *errAuditRecorder) Record(_ audit.Event) error { return fmt.Errorf("audit failed") }
func (e *errAuditRecorder) List() []audit.Event        { return nil }
