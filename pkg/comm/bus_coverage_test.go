package comm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// ---- Subscribe broadcast (agentID="") ----

func TestSubscribe_Broadcast(t *testing.T) {
	bus := NewInMemoryBus()
	var received []protocol.Message
	var mu sync.Mutex

	bus.Subscribe("", func(_ context.Context, msg protocol.Message) {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
	})

	bus.Publish(context.Background(), protocol.Message{
		ID:   "msg1",
		To:   "some_agent",
		Type: "test",
	})
	bus.Publish(context.Background(), protocol.Message{
		ID:   "msg2",
		To:   "other_agent",
		Type: "test",
	})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Errorf("broadcast should receive all messages: got %d, want 2", len(received))
	}
}

// ---- Subscribe broadcast unsubscribe ----

func TestSubscribe_BroadcastUnsubscribe(t *testing.T) {
	bus := NewInMemoryBus()
	var count int
	var mu sync.Mutex

	unsub := bus.Subscribe("", func(_ context.Context, _ protocol.Message) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	bus.Publish(context.Background(), protocol.Message{ID: "msg1", To: "agent"})
	time.Sleep(50 * time.Millisecond)

	unsub()
	// Double unsub should be safe.
	unsub()

	bus.Publish(context.Background(), protocol.Message{ID: "msg2", To: "agent"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 message before unsub, got %d", count)
	}
}

// ---- Subscribe targeted unsubscribe ----

func TestSubscribe_TargetedUnsubscribe(t *testing.T) {
	bus := NewInMemoryBus()
	var count int
	var mu sync.Mutex

	unsub := bus.Subscribe("agent1", func(_ context.Context, _ protocol.Message) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	bus.Publish(context.Background(), protocol.Message{ID: "msg1", To: "agent1"})
	time.Sleep(50 * time.Millisecond)

	unsub()
	unsub() // idempotent

	bus.Publish(context.Background(), protocol.Message{ID: "msg2", To: "agent1"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 message before unsub, got %d", count)
	}
}

// ---- Publish with empty To (broadcast only) ----

func TestPublish_EmptyTo(t *testing.T) {
	bus := NewInMemoryBus()
	var received int
	var mu sync.Mutex

	bus.Subscribe("", func(_ context.Context, _ protocol.Message) {
		mu.Lock()
		received++
		mu.Unlock()
	})

	// Message with empty To — targeted delivery is a no-op, but broadcast fires.
	bus.Publish(context.Background(), protocol.Message{ID: "msg1", To: ""})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if received != 1 {
		t.Errorf("broadcast should receive empty-To messages: got %d", received)
	}
}

// ---- Concurrent publish ----

func TestPublish_Concurrent(t *testing.T) {
	bus := NewInMemoryBus()
	var count int
	var mu sync.Mutex

	bus.Subscribe("target", func(_ context.Context, _ protocol.Message) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			bus.Publish(context.Background(), protocol.Message{
				ID: fmt.Sprintf("msg-%d", n),
				To: "target",
			})
		}(i)
	}
	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count != 100 {
		t.Errorf("expected 100 messages, got %d", count)
	}
}

// ---- Duplicate subscription ----

func TestSubscribe_Duplicate(t *testing.T) {
	bus := NewInMemoryBus()
	var count int
	var mu sync.Mutex

	h := func(_ context.Context, _ protocol.Message) {
		mu.Lock()
		count++
		mu.Unlock()
	}

	bus.Subscribe("agent1", h)
	bus.Subscribe("agent1", h)

	bus.Publish(context.Background(), protocol.Message{ID: "msg1", To: "agent1"})
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count != 2 {
		t.Errorf("duplicate subscription should deliver twice: got %d", count)
	}
}

// ---- NATSBus stub (0%) ----

func TestNATSBus_Stub(t *testing.T) {
	bus, err := NewNATSBus()
	if err == nil {
		t.Error("expected error from NATS stub")
	}
	if bus != nil {
		t.Error("expected nil bus from NATS stub")
	}

	// Exercise the stub methods to get coverage (they are no-ops).
	var stub NATSBus
	unsub := stub.Subscribe("agent1", func(_ context.Context, _ protocol.Message) {})
	if unsub == nil {
		t.Error("Subscribe should return non-nil unsubscribe")
	}
	unsub() // Should not panic.

	stub.Publish(context.Background(), protocol.Message{}) // Should not panic.
}

// ---- PanicHook ----

func TestSetPanicHook_NilClear(t *testing.T) {
	// Set then clear.
	SetPanicHook(func(_ any, _ []byte, _ protocol.Message) {})
	SetPanicHook(nil)

	fn := currentPanicHook()
	if fn != nil {
		t.Error("expected nil after clearing")
	}
}

// ---- safeHandle panic recovery ----

func TestSafeHandle_PanicRecovery(t *testing.T) {
	var hookCalled bool
	var mu sync.Mutex

	SetPanicHook(func(recovered any, stack []byte, msg protocol.Message) {
		mu.Lock()
		hookCalled = true
		mu.Unlock()
	})
	defer SetPanicHook(nil)

	msg := protocol.Message{ID: "panic-msg", To: "agent", Type: "test"}
	go safeHandle(context.Background(), msg, func(_ context.Context, _ protocol.Message) {
		panic("test panic")
	})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !hookCalled {
		t.Error("expected panic hook to be called")
	}
}
