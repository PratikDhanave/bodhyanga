package comm

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sync"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// Handler receives messages published on the bus.
//
// In more advanced systems this is often a method on a subscriber object that
// can maintain state and expose lifecycle hooks. A simple function keeps the
// demo compact and readable.
type Handler func(ctx context.Context, msg protocol.Message)

// Bus is a simple pub/sub message bus for agents.
//
// Conceptually:
// - Subscribe defines "who is interested in what?"
// - Publish defines "a new event occurred; deliver it"
//
// This interface is intentionally small so you can later swap an in-memory bus
// with a distributed transport without touching agent code.
type Bus interface {
	// Subscribe registers a handler for messages addressed to a specific agentID.
	//
	// The returned function removes the subscription. (In long-running systems
	// you need this for shutdown/draining and dynamic membership.)
	Subscribe(agentID string, h Handler) (unsubscribe func())

	// Publish routes the message to matching subscribers.
	Publish(ctx context.Context, msg protocol.Message)
}

// InMemoryBus is a basic in-memory implementation of Bus.
//
// Routing rules used here:
// - If msg.To is non-empty: deliver to subscribers of that agentID.
// - Always deliver to broadcast subscribers (agentID == "").
//
// Concurrency model:
// - Subscription lists are guarded by an RWMutex.
// - Handler invocation happens in separate goroutines to avoid blocking Publish.
//
// Important caveats (by design for demo simplicity):
// - No backpressure: fast publishers can overwhelm slow handlers.
// - No ordering guarantees: goroutine scheduling may reorder delivery.
// - No retries: handler panics/errors are not managed here.
type InMemoryBus struct {
	mu        sync.RWMutex
	subs      map[string][]Handler
	broadcast []Handler
}

// NewInMemoryBus constructs a new in-memory bus.
//
// The returned bus is safe for concurrent use.
func NewInMemoryBus() *InMemoryBus {
	return &InMemoryBus{
		subs: make(map[string][]Handler),
	}
}

// Subscribe registers a handler for messages addressed to a specific agent ID.
// If agentID is empty, the handler receives all messages (broadcast).
func (b *InMemoryBus) Subscribe(agentID string, h Handler) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if agentID == "" {
		b.broadcast = append(b.broadcast, h)
		idx := len(b.broadcast) - 1
		return func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if idx >= 0 && idx < len(b.broadcast) {
				b.broadcast[idx] = nil
			}
		}
	}
	b.subs[agentID] = append(b.subs[agentID], h)
	idx := len(b.subs[agentID]) - 1
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if idx >= 0 && idx < len(b.subs[agentID]) {
			b.subs[agentID][idx] = nil
		}
	}
}

// Publish sends a message to subscribed handlers.
//
// Message delivery is asynchronous (goroutines). This keeps the demo simple
// and avoids accidental deadlocks (e.g., a handler that publishes another
// message while holding locks).
//
// Handler panics are recovered to stderr so one buggy subscriber cannot
// crash the whole process. Other concerns (context propagation, buffering,
// backpressure) are still left to a production bus implementation.
func (b *InMemoryBus) Publish(ctx context.Context, msg protocol.Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Targeted subscribers.
	if msg.To != "" {
		for _, h := range b.subs[msg.To] {
			if h != nil {
				go safeHandle(ctx, msg, h)
			}
		}
	}

	// Broadcast subscribers see all messages.
	for _, h := range b.broadcast {
		if h != nil {
			go safeHandle(ctx, msg, h)
		}
	}
}

// panicHookMu guards access to the package-level panic hook so concurrent
// handler goroutines never race with SetPanicHook callers (notably tests
// resetting the hook in t.Cleanup).
var (
	panicHookMu sync.Mutex
	panicHook   func(recovered any, stack []byte, msg protocol.Message)
)

// SetPanicHook installs a function called whenever a subscriber's handler
// panics. It is invoked with the panic value, the goroutine's stack trace
// at the point of recovery, and the message that triggered the panic.
// Tests use this to assert the recovery path; production code can wire it
// to OTel / Sentry. Pass nil to clear.
func SetPanicHook(fn func(recovered any, stack []byte, msg protocol.Message)) {
	panicHookMu.Lock()
	panicHook = fn
	panicHookMu.Unlock()
}

func currentPanicHook() func(recovered any, stack []byte, msg protocol.Message) {
	panicHookMu.Lock()
	defer panicHookMu.Unlock()
	return panicHook
}

func safeHandle(ctx context.Context, msg protocol.Message, h Handler) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			if fn := currentPanicHook(); fn != nil {
				fn(r, stack, msg)
			}
			fmt.Fprintf(os.Stderr,
				"comm.InMemoryBus: handler panic for msg %s -> %s (type %s): %v\n%s",
				msg.ID, msg.To, msg.Type, r, stack)
		}
	}()
	h(ctx, msg)
}

var _ Bus = (*InMemoryBus)(nil)

