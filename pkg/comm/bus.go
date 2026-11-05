package comm

// File bus.go: the Bus interface (MARA interface 2 of 5) and its
// InMemoryBus implementation. See doc.go for the package-level overview.
//
// Bus transports protocol.Message values between agents that the
// orchestrator has subscribed. Agents NEVER call Publish directly — only
// the orchestrator publishes, after running governance and audit on the
// outgoing message. That separation (see Design rule 1 in
// docs/architecture.md § Design rules from MARA) is why every interaction
// in Bodhyanga ends up in the HIPAA audit log without the agents knowing about
// the log.
//
// The shipped InMemoryBus is single-process / demo-grade. Production swaps
// in Kafka, NATS JetStream, RabbitMQ, GCP Pub/Sub, or AWS EventBridge
// behind this same interface — see docs/operations.md § Bus
// implementations. Agents do not change when the bus changes.

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sync"

	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
)

// Handler receives messages published on the bus.
//
// Why a function and not an interface: the orchestrator wraps each agent in
// a closure (see pkg/orchestration/orchestrator.go Start) that already
// performs governance + audit + Agent.HandleMessage + republish. The bus
// just needs to invoke that closure — it doesn't need lifecycle hooks of
// its own. Production buses with subscriber state (e.g. Kafka consumer
// groups) can wrap their lifecycle around Subscribe/unsubscribe.
type Handler func(ctx context.Context, msg protocol.Message)

// Bus is a simple pub/sub message bus for agents.
//
// Conceptually:
//   - Subscribe defines "who is interested in what?"
//   - Publish defines "a new event occurred; deliver it"
//
// Routing semantics: Publish looks at msg.To and delivers to every
// subscriber registered for that exact agent ID, PLUS every "broadcast"
// subscriber (agentID == ""). Broadcast subscribers exist primarily for
// observability sinks (audit fan-out, OTel emit, dev console tail). There
// is no pattern-matching, no topic hierarchy — routing is by string equal.
//
// This interface is intentionally small so you can later swap an in-memory
// bus with a distributed transport without touching agent code. The
// substitution is at construction time in cmd/care/main.go (or wherever
// the bus is wired) — every agents/*.go file is bus-implementation-blind.
type Bus interface {
	// Subscribe registers a handler for messages addressed to a specific
	// agentID. Pass "" as agentID to receive every message (broadcast) —
	// used by observability sinks, not by clinical agents.
	//
	// The returned function removes the subscription. In long-running
	// systems you need this for shutdown / draining and dynamic agent
	// membership; tests use it to tear down between cases. Calling
	// Subscribe twice for the same agentID + handler is permitted (no
	// dedup) — callers that need uniqueness must enforce it themselves.
	Subscribe(agentID string, h Handler) (unsubscribe func())

	// Publish routes the message to matching subscribers. The
	// orchestrator is the canonical caller; agents are not.
	Publish(ctx context.Context, msg protocol.Message)
}

// InMemoryBus is a basic in-memory implementation of Bus.
//
// Routing rules used here:
//   - If msg.To is non-empty: deliver to subscribers of that agentID.
//   - Always deliver to broadcast subscribers (agentID == "").
//
// Concurrency model:
//   - Subscription lists are guarded by an RWMutex (cheap concurrent
//     Publish, slightly more expensive Subscribe / unsubscribe).
//   - Handler invocation happens in **separate goroutines** so a slow
//     handler can't block Publish (and can't deadlock callers that
//     publish from inside another handler).
//
// Important caveats (by design for demo simplicity — these become
// production concerns the moment you replace this impl):
//
//   - **No FIFO guarantee.** Two Publish calls A then B may have their
//     handlers invoked in B-then-A order because goroutines are scheduled
//     by the runtime. The application protocol (state machines in
//     supervisors) does ordering work today — see docs/architecture.md
//     § Concurrency and ordering. The diagnostic supervisor explicitly
//     waits for questions_complete before forwarding to test_planner,
//     etc., so the order on the bus doesn't matter at the case level.
//   - **No backpressure.** A fast publisher can spawn unbounded
//     goroutines if handlers are slow. Production buses must surface
//     queue depth and apply per-consumer flow control.
//   - **No retries.** Handler panics are recovered (see safeHandle), but
//     the message is not redelivered. Retry policy is a production-bus
//     concern.
//   - **No persistence.** Messages live only as long as the in-flight
//     goroutine. A process crash loses them. Production buses persist
//     and replay.
//   - **No msg dedup / idempotency.** The bus delivers exactly as many
//     times as Subscribe was called. Idempotent handlers are the
//     architecturally simpler answer.
type InMemoryBus struct {
	mu        sync.RWMutex
	subs      map[string][]Handler
	broadcast []Handler
}

// NewInMemoryBus constructs a new in-memory bus.
//
// The returned bus is safe for concurrent use from the moment it's
// returned. Callers typically create one bus per process and inject it
// into the orchestrator (see cmd/demo/main.go / cmd/care/main.go).
func NewInMemoryBus() *InMemoryBus {
	return &InMemoryBus{
		subs: make(map[string][]Handler),
	}
}

// Subscribe registers a handler for messages addressed to a specific
// agent ID. If agentID is empty, the handler receives all messages
// (broadcast).
//
// Idempotency note: re-subscribing the same agentID with the same handler
// is allowed and produces TWO deliveries per Publish. The orchestrator
// guards against double-subscription with its startOnce + subscribed map
// (see pkg/orchestration/orchestrator.go Start). Callers wiring their own
// subscriptions must enforce uniqueness if they need it.
//
// The returned unsubscribe is index-based and idempotent — calling it
// twice is safe; the second call sees a nil slot and no-ops. It does NOT
// compact the slice (we leave nil holes) because compacting would shift
// every subsequent subscriber's index and break their unsubscribe
// closures. The trade-off is a slow drift toward longer subscriber
// slices over long-running processes; acceptable for the demo bus.
func (b *InMemoryBus) Subscribe(agentID string, h Handler) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Broadcast path: subscribers under "" get every message regardless
	// of msg.To. Used by observability sinks that want a tail of the
	// whole bus. NOT used by clinical agents — they always subscribe
	// to their own ID.
	if agentID == "" {
		b.broadcast = append(b.broadcast, h)
		idx := len(b.broadcast) - 1
		return func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			// Bounds-check survives double-unsubscribe and racing
			// resets; we never shift the slice so idx remains valid.
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
// Message delivery is asynchronous (one goroutine per matched
// subscriber). This keeps the demo simple and avoids accidental
// deadlocks (e.g. a handler that publishes another message while the
// caller still holds the read lock — without the goroutine hop, the
// nested Publish would block trying to take the same lock).
//
// Handler panics are recovered to stderr (see safeHandle) so one buggy
// subscriber cannot crash the whole process. Other concerns (context
// propagation under cancellation, buffering, backpressure, retries) are
// left to a production bus implementation. See docs/operations.md
// § Bus implementations for the Kafka / NATS / RabbitMQ swap matrix.
//
// Important: this method is NOT how a production bus actually works.
// Kafka commits offsets, NATS acks JetStream messages, RabbitMQ confirms
// — all of those require persistent state the in-memory bus doesn't have.
// What this implementation buys you is the same SHAPE (Subscribe /
// Publish / Handler) so the swap is a constructor change, not a refactor.
func (b *InMemoryBus) Publish(ctx context.Context, msg protocol.Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Targeted subscribers. Routing is by msg.To string-equal — there is
	// no pattern matching, no glob, no topic hierarchy. The orchestrator
	// composes msg.To from agent.ID() values, so the match is exact.
	if msg.To != "" {
		for _, h := range b.subs[msg.To] {
			// nil slot = an earlier unsubscribe; skip without
			// compacting (see Subscribe rationale).
			if h != nil {
				go safeHandle(ctx, msg, h)
			}
		}
	}

	// Broadcast subscribers see EVERY message, regardless of msg.To
	// (including the empty-To case where targeted delivery was a no-op).
	// This is what makes them useful for audit / OTel fan-out.
	for _, h := range b.broadcast {
		if h != nil {
			go safeHandle(ctx, msg, h)
		}
	}
}

// panicHookMu guards access to the package-level panic hook so concurrent
// handler goroutines never race with SetPanicHook callers (notably tests
// resetting the hook in t.Cleanup, which would otherwise data-race the
// goroutine that's mid-recover).
var (
	panicHookMu sync.Mutex
	panicHook   func(recovered any, stack []byte, msg protocol.Message)
)

// SetPanicHook installs a function called whenever a subscriber's handler
// panics. It is invoked with the panic value, the goroutine's stack trace
// at the point of recovery, and the message that triggered the panic.
//
// Two production uses:
//
//   - Wire to Sentry / OTel exception export so a panic in any agent
//     becomes a tracked incident, not a stderr line that scrolls past.
//   - Wire to a test assertion that fails the test when an agent panics
//     instead of letting the recovery silently swallow the bug.
//
// Pass nil to clear the hook. Concurrent calls are safe.
func SetPanicHook(fn func(recovered any, stack []byte, msg protocol.Message)) {
	panicHookMu.Lock()
	panicHook = fn
	panicHookMu.Unlock()
}

// currentPanicHook returns the installed hook under the package mutex so
// readers see a coherent snapshot — the alternative (atomic.Value) was
// rejected because the hook surface is tiny and contention is negligible.
func currentPanicHook() func(recovered any, stack []byte, msg protocol.Message) {
	panicHookMu.Lock()
	defer panicHookMu.Unlock()
	return panicHook
}

// safeHandle is the panic-recovery wrapper added in commit 1b8ce62. It
// ensures one buggy agent's panic cannot crash the whole process — every
// other agent (and every subsequent message for the same agent) keeps
// processing.
//
// Why this exists: HandleMessage can do arbitrary work — JSON decode of
// untrusted input, LLM client calls, regex parsing of simulated lab
// strings, map lookups against case state. Any of those can panic on a
// bad case. The orchestrator's KindAgentError audit path catches returned
// errors but NOT panics; without this recovery, a single nil deref in a
// test_planner subscriber would kill cmd/care for every tenant on the
// box. Defence in depth lives at the bus boundary.
//
// What this does NOT do: it does not retry, does not republish, does not
// notify the supervisor that an agent went sideways. The case ends
// silently from the supervisor's perspective (it never receives the
// expected follow-up message). That's an acceptable trade-off for a
// research demo; production audit pipelines would surface the panic as a
// KindAgentError event via the panic hook.
func safeHandle(ctx context.Context, msg protocol.Message, h Handler) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			if fn := currentPanicHook(); fn != nil {
				// Hook is best-effort; if it itself panics we
				// fall through to the stderr write below.
				fn(r, stack, msg)
			}
			fmt.Fprintf(os.Stderr,
				"comm.InMemoryBus: handler panic for msg %s -> %s (type %s): %v\n%s",
				msg.ID, msg.To, msg.Type, r, stack)
		}
	}()
	h(ctx, msg)
}

// Compile-time assertion that InMemoryBus satisfies Bus. If this stops
// compiling, the interface and the implementation have drifted.
var _ Bus = (*InMemoryBus)(nil)
