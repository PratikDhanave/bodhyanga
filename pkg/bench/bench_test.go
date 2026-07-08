// Package bench provides performance benchmarks for Bodh's core subsystems.
//
// Run all benchmarks:
//
//	go test ./pkg/bench/... -bench=. -benchtime=5s
//
// Run a specific benchmark:
//
//	go test ./pkg/bench/... -bench=BenchmarkBusThroughput -benchtime=5s
//
// With per-iteration latency histogram output:
//
//	go test ./pkg/bench/... -bench=. -benchmem -v
package bench

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
	"github.com/PratikDhanave/bodh/pkg/protocol"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

// ----------------------------------------------------------------------------
// Shared test helpers
// ----------------------------------------------------------------------------

// noopAgent is a zero-overhead agent that records call counts but does no work.
type noopAgent struct {
	id    string
	name  string
	caps  []string
	calls int64
}

func (a *noopAgent) ID() string             { return a.id }
func (a *noopAgent) Name() string           { return a.name }
func (a *noopAgent) Capabilities() []string { return a.caps }
func (a *noopAgent) HandleMessage(_ context.Context, _ agent.Message, _ agent.Environment) ([]agent.Message, error) {
	atomic.AddInt64(&a.calls, 1)
	return nil, nil
}

// forwardingAgent routes each received message to `nextAgent`, enabling
// multi-hop pipeline benchmarks without LLM round-trips.
type forwardingAgent struct {
	id        string
	name      string
	caps      []string
	nextAgent string
	calls     int64
	done      chan struct{} // closed when doneAfter calls have been processed
	doneAfter int64
	once      sync.Once
}

func (a *forwardingAgent) ID() string             { return a.id }
func (a *forwardingAgent) Name() string           { return a.name }
func (a *forwardingAgent) Capabilities() []string { return a.caps }
func (a *forwardingAgent) HandleMessage(_ context.Context, msg agent.Message, _ agent.Environment) ([]agent.Message, error) {
	n := atomic.AddInt64(&a.calls, 1)
	if a.nextAgent == "" {
		// Terminal agent: signal done when we've received the expected count.
		if n >= a.doneAfter {
			a.once.Do(func() { close(a.done) })
		}
		return nil, nil
	}
	out := agent.NewMessage(a.id, a.nextAgent, agent.RoleAgent, "hop", msg.Content, msg.Metadata)
	return []agent.Message{out}, nil
}

// allowAll is a minimal governance.Policy that allows every message (zero overhead
// beyond the interface dispatch). Used in bus and orchestrator benchmarks to
// eliminate policy overhead from the measurement.
type allowAll struct{}

func (allowAll) Evaluate(_ context.Context, _ protocol.Message) (governance.PolicyResult, error) {
	return governance.PolicyResult{
		Decision:  governance.DecisionAllow,
		Reason:    "allow-all",
		CheckedAt: time.Now().UTC(),
	}, nil
}

// discardEnv is an agent.Environment that silently drops all log output and
// returns time.Now(). Using it removes the cost of a real logger from bench
// measurements of bus/orchestrator throughput.
type discardEnv struct{}

func (discardEnv) Now() time.Time      { return time.Now() }
func (discardEnv) Logf(string, ...any) {}

// ----------------------------------------------------------------------------
// BenchmarkBusThroughput
// ----------------------------------------------------------------------------

// BenchmarkBusThroughput measures raw publish-to-handler throughput of
// InMemoryBus for 1, 5, and 10 subscribers on the same target address.
//
// Each sub-benchmark publishes b.N messages and blocks until every handler
// has been called, giving us:
//   - messages/s delivered (implicit in ns/op)
//   - how delivery throughput scales as subscriber count grows
func BenchmarkBusThroughput(b *testing.B) {
	for _, n := range []int{1, 5, 10} {
		b.Run(fmt.Sprintf("subscribers_%d", n), func(b *testing.B) {
			bus := comm.NewInMemoryBus()
			target := "bench-target"

			// Track how many handlers have fired; we need n*b.N total.
			var got int64
			expected := int64(n) * int64(b.N)
			done := make(chan struct{})
			var once sync.Once

			for i := 0; i < n; i++ {
				bus.Subscribe(target, func(_ context.Context, _ protocol.Message) {
					if v := atomic.AddInt64(&got, 1); v >= expected {
						once.Do(func() { close(done) })
					}
				})
			}

			msg := agent.NewMessage("publisher", target, agent.RoleAgent, "bench", "payload", nil)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bus.Publish(context.Background(), msg)
			}
			// Wait for all async goroutines to complete before stopping
			// the timer to get a fair end-to-end latency number.
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				b.Fatalf("timed out waiting for %d deliveries", expected)
			}
			b.StopTimer()
		})
	}
}

// BenchmarkBusThroughputLatency supplements BenchmarkBusThroughput with a
// hand-rolled p50/p95/p99 histogram for 1-subscriber delivery latency.
// It uses testing.B.N iterations so the Go benchmark harness can scale the
// run time automatically.
func BenchmarkBusThroughputLatency(b *testing.B) {
	bus := comm.NewInMemoryBus()
	target := "latency-target"
	latencies := make([]time.Duration, 0, b.N)
	var mu sync.Mutex
	delivered := make(chan struct{}, b.N)

	bus.Subscribe(target, func(_ context.Context, msg protocol.Message) {
		// Recover send time embedded in metadata.
		if ts, ok := msg.Metadata["_ts"].(int64); ok {
			d := time.Since(time.Unix(0, ts))
			mu.Lock()
			latencies = append(latencies, d)
			mu.Unlock()
		}
		delivered <- struct{}{}
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := agent.NewMessage("pub", target, agent.RoleAgent, "bench", "x",
			map[string]any{"_ts": time.Now().UnixNano()})
		bus.Publish(context.Background(), msg)
		<-delivered // wait for this delivery before sending next (serial)
	}
	b.StopTimer()

	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		p50 := latencies[len(latencies)*50/100]
		p95 := latencies[len(latencies)*95/100]
		p99 := latencies[len(latencies)*99/100]
		b.ReportMetric(float64(p50.Microseconds()), "p50_us")
		b.ReportMetric(float64(p95.Microseconds()), "p95_us")
		b.ReportMetric(float64(p99.Microseconds()), "p99_us")
	}
}

// ----------------------------------------------------------------------------
// BenchmarkOrchestratorThroughput
// ----------------------------------------------------------------------------

// BenchmarkOrchestratorThroughput measures how many end-to-end case
// completions the orchestrator + bus + agents can sustain.  The pipeline
// is: publisher → intake → diagnostician → reporter (terminal).
//
// The reporter signals completion via a channel; we wait for all N completions
// before stopping the timer.
func BenchmarkOrchestratorThroughput(b *testing.B) {
	ctx := context.Background()

	// Terminal agent: counts completions.
	reporter := &forwardingAgent{
		id:        "reporter",
		name:      "reporter",
		caps:      []string{"report"},
		nextAgent: "", // terminal
		doneAfter: int64(b.N),
		done:      make(chan struct{}),
	}
	// Middle agent: forwards to reporter.
	diag := &forwardingAgent{
		id:        "diagnostician",
		name:      "diagnostician",
		caps:      []string{"diagnose"},
		nextAgent: "reporter",
	}
	// Entry agent: forwards to diagnostician.
	intake := &forwardingAgent{
		id:        "intake",
		name:      "intake",
		caps:      []string{"intake"},
		nextAgent: "diagnostician",
	}

	reg := registry.NewInMemory()
	for _, a := range []agent.Agent{intake, diag, reporter} {
		if err := reg.Register(ctx, a); err != nil {
			b.Fatalf("register: %v", err)
		}
	}

	bus := comm.NewInMemoryBus()
	env := discardEnv{}
	orch := orchestration.NewOrchestrator(reg, bus, allowAll{}, env)
	orch.Start(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := agent.NewMessage("user", "intake", agent.RoleUser, "case",
			"patient case", map[string]any{
				"case_id": fmt.Sprintf("BENCH-%06d", i),
			})
		bus.Publish(ctx, msg)
	}
	select {
	case <-reporter.done:
	case <-time.After(60 * time.Second):
		b.Fatalf("timed out: only %d/%d cases completed", atomic.LoadInt64(&reporter.calls), b.N)
	}
	b.StopTimer()

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "cases/s")
}

// ----------------------------------------------------------------------------
// BenchmarkLLMClientMock
// ----------------------------------------------------------------------------

// BenchmarkLLMClientMock compares raw *llm.Mock Complete calls against the
// same calls wrapped in observability.TracingClient.  The difference in ns/op
// is the cost of the tracing wrapper (timestamp, struct alloc, RecordLLMCall).
func BenchmarkLLMClientMock(b *testing.B) {
	req := llm.Request{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		MaxTokens: 16,
	}
	ctx := context.Background()

	b.Run("raw_mock", func(b *testing.B) {
		m := &llm.Mock{Resp: &llm.Response{Content: "ok", Provider: "mock", Model: "mock-model"}}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := m.Complete(ctx, req); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("tracing_client", func(b *testing.B) {
		m := &llm.Mock{Resp: &llm.Response{Content: "ok", Provider: "mock", Model: "mock-model"}}
		logger := observability.NewLoggerTo(io.Discard)
		tc := observability.NewTracingClient(m, logger, "bench-agent")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := tc.Complete(ctx, req); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// ----------------------------------------------------------------------------
// BenchmarkRegistryLookup
// ----------------------------------------------------------------------------

// BenchmarkRegistryLookup measures FindByCapability latency as the registry
// grows from 10 to 1 000 agents.  The target capability is always present
// (worst-case scan to the end of the agent list).
func BenchmarkRegistryLookup(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("agents_%d", n), func(b *testing.B) {
			ctx := context.Background()
			reg := registry.NewInMemory()
			needle := "special-cap"
			for i := 0; i < n-1; i++ {
				a := &noopAgent{
					id:   fmt.Sprintf("agent-%04d", i),
					name: fmt.Sprintf("Agent %d", i),
					caps: []string{fmt.Sprintf("cap-%d", i)},
				}
				if err := reg.Register(ctx, a); err != nil {
					b.Fatalf("register: %v", err)
				}
			}
			// The last agent has the needle capability (worst-case scan).
			last := &noopAgent{
				id:   fmt.Sprintf("agent-%04d", n-1),
				name: fmt.Sprintf("Agent %d", n-1),
				caps: []string{needle},
			}
			if err := reg.Register(ctx, last); err != nil {
				b.Fatalf("register: %v", err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got := reg.FindByCapability(ctx, needle)
				if len(got) != 1 {
					b.Fatalf("expected 1 agent, got %d", len(got))
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// BenchmarkGovernanceEval
// ----------------------------------------------------------------------------

// BenchmarkGovernanceEval measures CompositePolicy.Evaluate overhead for a
// stack of 5 chained sub-policies.  The goal is sub-1µs for in-process
// governance since every message hop pays this cost synchronously.
func BenchmarkGovernanceEval(b *testing.B) {
	// Build a composite of 5 allowAll sub-policies to simulate a real
	// deployment that stacks RequireCaseIDPolicy, MaxContentLengthPolicy,
	// RequireTenantPolicy, RateLimitPolicy, Part2Policy, etc.
	policies := make([]governance.Policy, 5)
	for i := range policies {
		policies[i] = allowAll{}
	}
	composite := governance.NewComposite(policies...)

	msg := agent.NewMessage("user", "intake", agent.RoleUser, "presentation",
		"patient presentation", map[string]any{"case_id": "BENCH-001"})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := composite.Evaluate(ctx, msg)
		if err != nil {
			b.Fatalf("evaluate: %v", err)
		}
		if res.Decision != governance.DecisionAllow {
			b.Fatalf("unexpected decision: %s", res.Decision)
		}
	}
}

// BenchmarkGovernanceEvalDeny measures the short-circuit fast path: the first
// policy denies so policies 2-5 are never called.
func BenchmarkGovernanceEvalDeny(b *testing.B) {
	denyFirst := governance.NewComposite(
		denyAll{},
		allowAll{}, allowAll{}, allowAll{}, allowAll{},
	)
	msg := agent.NewMessage("user", "intake", agent.RoleUser, "presentation",
		"patient presentation", map[string]any{"case_id": "BENCH-001"})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := denyFirst.Evaluate(ctx, msg)
		if err != nil {
			b.Fatalf("evaluate: %v", err)
		}
		if res.Decision != governance.DecisionDeny {
			b.Fatalf("unexpected decision: %s", res.Decision)
		}
	}
}

// denyAll denies every message; used in BenchmarkGovernanceEvalDeny.
type denyAll struct{}

func (denyAll) Evaluate(_ context.Context, _ protocol.Message) (governance.PolicyResult, error) {
	return governance.PolicyResult{
		Decision:  governance.DecisionDeny,
		Reason:    "deny-all",
		CheckedAt: time.Now().UTC(),
	}, nil
}
