//go:build nats

package comm

// Integration tests for the NATSBus at-least-once delivery semantics. They run
// only under `-tags nats` and require Docker (a real NATS JetStream container
// via testcontainers). They are the empirical proof for decoupling PR-0.7:
// the previous implementation acked BEFORE running the handler (at-most-once;
// a crash mid-handle silently lost the message). These assert the corrected
// behaviour — ack only after success, redeliver on failure, dead-letter poison.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// startNATS spins up a NATS JetStream container and returns its client URL.
func startNATS(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "nats:2.10",
		Cmd:          []string{"-js"},
		ExposedPorts: []string{"4222/tcp"},
		WaitingFor:   wait.ForListeningPort("4222/tcp").WithStartupTimeout(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start nats container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := c.MappedPort(ctx, "4222/tcp")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	return fmt.Sprintf("nats://%s:%s", host, port.Port())
}

// newBus wires a NATSBus against the container, scoped to a unique tenant.
func newBus(t *testing.T, tenant string) *NATSBus {
	t.Helper()
	os.Setenv("NATS_URL", startNATS(t))
	os.Setenv("BODH_TENANT_ID", tenant)
	bus, err := NewNATSBus()
	if err != nil {
		t.Fatalf("NewNATSBus: %v", err)
	}
	t.Cleanup(func() { _ = bus.Close() })
	return bus
}

// TestNATSBus_RedeliversOnHandlerFailure is the core at-least-once proof: a
// handler that panics on its first delivery must see the SAME message again
// (Nak → redelivery), and the message must NOT be lost.
func TestNATSBus_RedeliversOnHandlerFailure(t *testing.T) {
	bus := newBus(t, "t-redeliver")

	var calls int32
	succeeded := make(chan struct{}, 1)
	unsub := bus.Subscribe("agent-x", func(_ context.Context, _ protocol.Message) {
		if atomic.AddInt32(&calls, 1) == 1 {
			panic("simulated crash on first delivery") // recovered by safeHandle → Nak
		}
		select {
		case succeeded <- struct{}{}:
		default:
		}
	})
	defer unsub()

	bus.Publish(context.Background(), protocol.Message{
		ID: "m-redeliver", To: "agent-x", Role: protocol.RoleUser, Type: "case_state", Content: "{}",
	})

	select {
	case <-succeeded:
	case <-time.After(20 * time.Second):
		t.Fatalf("message was NOT redelivered after handler failure (calls=%d) — at-most-once regression",
			atomic.LoadInt32(&calls))
	}
	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Fatalf("expected >=2 deliveries (redelivery), got %d", got)
	}
}

// TestNATSBus_AcksOnSuccess proves the happy path acks exactly once: a
// successful handler must not see the message redelivered.
func TestNATSBus_AcksOnSuccess(t *testing.T) {
	bus := newBus(t, "t-ack")

	var calls int32
	unsub := bus.Subscribe("agent-y", func(_ context.Context, _ protocol.Message) {
		atomic.AddInt32(&calls, 1)
	})
	defer unsub()

	bus.Publish(context.Background(), protocol.Message{
		ID: "m-ack", To: "agent-y", Role: protocol.RoleUser, Type: "case_state", Content: "{}",
	})

	// Let it process and settle; a missing ack would (eventually) redeliver,
	// but a successful ack means the count stays at 1.
	time.Sleep(4 * time.Second)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("successful handler should be delivered exactly once, got %d", got)
	}
}

// TestNATSBus_DeadLettersPoison proves an unparseable message never reaches the
// handler and lands in the DLQ subject with a reason header (instead of looping
// forever or being silently acked-and-dropped).
func TestNATSBus_DeadLettersPoison(t *testing.T) {
	bus := newBus(t, "t-dlq")
	ctx := context.Background()

	var calls int32
	unsub := bus.Subscribe("agent-z", func(_ context.Context, _ protocol.Message) {
		atomic.AddInt32(&calls, 1)
	})
	defer unsub()

	// Publish RAW non-JSON bytes straight to the agent subject (bypassing
	// Publish, which would marshal a valid Message).
	if _, err := bus.js.Publish(ctx, bus.subject("agent-z"), []byte("{ this is not valid json")); err != nil {
		t.Fatalf("raw publish: %v", err)
	}

	// Consume the DLQ subject and expect the poison message with its reason.
	dlqSubject := fmt.Sprintf("%s.%s.%s.%s", natsSubjectPrefix, "t-dlq", natsDLQSegment, "agent-z")
	dlq, err := bus.js.CreateOrUpdateConsumer(ctx, natsStreamName, jetstream.ConsumerConfig{
		FilterSubject: dlqSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		t.Fatalf("create dlq consumer: %v", err)
	}

	msgs, err := dlq.Fetch(1, jetstream.FetchMaxWait(10*time.Second))
	if err != nil {
		t.Fatalf("fetch dlq: %v", err)
	}
	var dlqCount int
	var reason string
	for m := range msgs.Messages() {
		dlqCount++
		reason = m.Headers().Get("X-Bodh-DLQ-Reason")
		_ = m.Ack()
	}
	if dlqCount != 1 {
		t.Fatalf("expected exactly 1 dead-lettered message, got %d", dlqCount)
	}
	if !strings.Contains(reason, "unmarshal") {
		t.Errorf("DLQ reason header = %q, want it to mention unmarshal", reason)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("poison message must never reach the handler, got %d calls", got)
	}
}
