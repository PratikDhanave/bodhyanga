// nats.go — NATS JetStream message bus adapter.
//
// This file is compiled only when the `nats` build tag is set:
//
//	go build -tags nats ./cmd/care
//
// Without the tag the stub in nats_stub.go is used instead, keeping the
// default binary free of the NATS client library.
//
// # Design
//
// NATSBus wraps a NATS JetStream connection and implements the Bus interface.
// Each call to Subscribe creates a durable JetStream push consumer that
// delivers to an internal channel; the handler goroutine drains that channel
// and calls the user-supplied Handler. Publish marshals protocol.Message to
// JSON and publishes to the per-recipient subject.
//
// Subject naming:
//
//	Point-to-point: bodh.{tenantID}.{agentID}
//	Broadcast:      bodh.{tenantID}.broadcast
//
// Stream:   BODH_MESSAGES  (subjects: bodh.>)
// Consumer: durable, AckExplicit, per-agent name derived from agentID.
//
// # Environment variables
//
//	NATS_URL       NATS server URL (default nats://localhost:4222)
//	BODH_TENANT_ID Tenant identifier used in subject prefix (default "default")
//
// # Limitations
//
// The current implementation uses the nats.go client library, which must be
// available in the module graph when built with -tags nats.  Add it with:
//
//	go get github.com/nats-io/nats.go@latest
//	go get github.com/nats-io/nats.go/jetstream@latest

//go:build nats

package comm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/protocol"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// natsStreamName is the JetStream stream that carries all bodh messages.
	// A single stream with a wildcard subject filter avoids the overhead of
	// per-tenant streams while keeping subjects namespaced.
	natsStreamName = "BODH_MESSAGES"

	// natsSubjectPrefix is the top-level subject prefix for all Bodh messages.
	natsSubjectPrefix = "bodh"

	// natsBroadcastSegment is the final subject segment used for broadcast
	// subscribers (agentID == "").
	natsBroadcastSegment = "broadcast"

	// natsAckWait is how long JetStream waits for an Ack before treating an
	// in-flight message as failed and redelivering it. This is the safety net
	// for a consumer that CRASHES mid-processing (no Ack, no Nak): the message
	// is redelivered to a live consumer after this window. Sized generously
	// because an agent handler may make a slow LLM call before acking.
	natsAckWait = 30 * time.Second

	// natsMaxDeliver caps redelivery attempts. After this many deliveries a
	// still-failing message is dead-lettered and Term'd rather than looping
	// forever (a poison message must not wedge a consumer).
	natsMaxDeliver = 5

	// natsDLQSegment namespaces dead-lettered messages:
	// bodh.{tenant}.dlq.{agentID}. DLQ subjects are stored in the stream (so
	// they are inspectable) but match no agent consumer's FilterSubject, so a
	// dead-lettered message is never redelivered to an agent.
	natsDLQSegment = "dlq"
)

// NATSBus implements Bus using NATS JetStream for durable, distributed
// message delivery.
//
// NATSBus is safe for concurrent use. Subscriptions are tracked so that
// the unsubscribe function returned by Subscribe can drain and delete the
// durable consumer cleanly.
type NATSBus struct {
	nc       *nats.Conn
	js       jetstream.JetStream
	tenantID string

	mu   sync.Mutex
	subs []*natsSub // all active subscriptions for cleanup on Close
}

// natsSub holds the state for a single active JetStream subscription.
type natsSub struct {
	consumer jetstream.Consumer
	cancel   context.CancelFunc // stops the consume goroutine
	done     chan struct{}       // closed when the goroutine exits
}

// NewNATSBus connects to a NATS server, ensures the BODH_MESSAGES stream
// exists, and returns a ready-to-use NATSBus.
//
// Connection URL is read from NATS_URL (default nats://localhost:4222).
// The tenant prefix is read from BODH_TENANT_ID (default "default").
//
// Returns (nil, error) on connection or stream-setup failure. The caller
// should fall back to NewInMemoryBus in that case.
func NewNATSBus() (*NATSBus, error) {
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = nats.DefaultURL // nats://localhost:4222
	}

	nc, err := nats.Connect(url,
		nats.Name("bodh-bus"),
		nats.Timeout(5*time.Second),
		nats.MaxReconnects(10),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("comm.NATSBus: connect to %s: %w", url, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("comm.NATSBus: create JetStream context: %w", err)
	}

	tenant := os.Getenv("BODH_TENANT_ID")
	if tenant == "" {
		tenant = "default"
	}

	bus := &NATSBus{
		nc:       nc,
		js:       js,
		tenantID: tenant,
	}

	if err := bus.ensureStream(context.Background()); err != nil {
		nc.Close()
		return nil, err
	}

	return bus, nil
}

// ensureStream creates or updates the BODH_MESSAGES stream. The stream
// covers all subjects under "bodh.>" so every tenant and agent subject is
// captured by a single stream. Calling this on an already-existing stream
// is idempotent.
func (b *NATSBus) ensureStream(ctx context.Context) error {
	cfg := jetstream.StreamConfig{
		Name:      natsStreamName,
		Subjects:  []string{natsSubjectPrefix + ".>"},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    24 * time.Hour,
		Storage:   jetstream.FileStorage,
		Replicas:  1,
	}

	_, err := b.js.CreateOrUpdateStream(ctx, cfg)
	if err != nil {
		return fmt.Errorf("comm.NATSBus: ensure stream %s: %w", natsStreamName, err)
	}
	return nil
}

// subject returns the NATS subject for the given agentID.
// Empty agentID maps to the broadcast subject.
func (b *NATSBus) subject(agentID string) string {
	if agentID == "" {
		return fmt.Sprintf("%s.%s.%s", natsSubjectPrefix, b.tenantID, natsBroadcastSegment)
	}
	// Sanitise agentID: NATS subjects cannot contain spaces or wildcards.
	safe := strings.NewReplacer(" ", "_", "*", "_", ">", "_").Replace(agentID)
	return fmt.Sprintf("%s.%s.%s", natsSubjectPrefix, b.tenantID, safe)
}

// consumerName derives a stable, NATS-friendly durable consumer name from
// the agentID. Consumer names must be alphanumeric + dash/underscore.
func consumerName(agentID string) string {
	if agentID == "" {
		return "bodh-broadcast"
	}
	r := strings.NewReplacer(
		" ", "-", "_", "-", ".", "-",
		"*", "x", ">", "x", "/", "-",
	)
	return "bodh-" + r.Replace(agentID)
}

// Subscribe creates a durable JetStream push consumer for the given agentID
// and starts a goroutine that calls h for each delivered message. The
// returned function cancels the consume loop and deletes the consumer.
//
// Routing contract (mirrors InMemoryBus):
//   - Non-empty agentID: receives messages whose msg.To == agentID.
//   - Empty agentID ("broadcast"): receives every message published on the
//     broadcast subject.
//
// Note: unlike InMemoryBus, a broadcast subscriber here only receives
// messages published via the broadcast subject path. If you need true
// fan-out of every message (including point-to-point), subscribe to the
// stream directly with a wildcard consumer — that is a deliberate design
// extension point, not a bug.
func (b *NATSBus) Subscribe(agentID string, h Handler) (unsubscribe func()) {
	subj := b.subject(agentID)
	durableName := consumerName(agentID)

	cfg := jetstream.ConsumerConfig{
		Name:          durableName,
		Durable:       durableName,
		FilterSubject: subj,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       natsAckWait,
		MaxDeliver:    natsMaxDeliver,
		DeliverPolicy: jetstream.DeliverNewPolicy,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	consumer, err := b.js.CreateOrUpdateConsumer(ctx, natsStreamName, cfg)
	if err != nil {
		cancel()
		// Return a no-op unsubscribe; the bus continues serving other subs.
		fmt.Fprintf(os.Stderr,
			"comm.NATSBus: subscribe agentID=%q: create consumer: %v\n", agentID, err)
		return func() {}
	}

	sub := &natsSub{consumer: consumer, cancel: cancel, done: done}

	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()

	go func() {
		defer close(done)
		for {
			// FetchNoWait returns immediately; use Fetch with a short
			// deadline so we can honour context cancellation promptly
			// without busy-spinning.
			msgs, err := consumer.Fetch(10, jetstream.FetchMaxWait(200*time.Millisecond))
			if err != nil {
				if ctx.Err() != nil {
					return // context cancelled: normal shutdown
				}
				// Transient error (server restart, etc.); back off briefly.
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
				continue
			}

			for msg := range msgs.Messages() {
				if ctx.Err() != nil {
					// Shutting down before we processed this one: Nak so it
					// redelivers to a live consumer rather than waiting out
					// AckWait. No message is dropped on shutdown.
					_ = msg.Nak()
					return
				}
				var pm protocol.Message
				if err := json.Unmarshal(msg.Data(), &pm); err != nil {
					// Poison message: it will never parse, so redelivery is
					// pointless and would loop until MaxDeliver. Dead-letter it
					// for inspection and Term to stop redelivery immediately.
					b.deadLetter(agentID, msg, "unmarshal: "+err.Error())
					_ = msg.Term()
					continue
				}
				// At-least-once core: process SYNCHRONOUSLY and Ack ONLY after
				// the handler returns without panicking. If the process crashes
				// or the handler panics before the Ack, the message stays
				// in-flight and JetStream redelivers it — so no clinical
				// message is silently lost (the at-most-once bug this replaces).
				// Handlers must therefore be idempotent; redelivery is expected.
				if safeHandle(ctx, pm, h) {
					_ = msg.Ack()
					continue
				}
				// Handler panicked (safeHandle recovered it). Redeliver via Nak
				// until MaxDeliver is reached, then dead-letter + Term so a
				// reproducibly-panicking message cannot wedge the consumer.
				if md, mderr := msg.Metadata(); mderr == nil && md.NumDelivered >= natsMaxDeliver {
					b.deadLetter(agentID, msg, "handler panicked; exhausted redeliveries")
					_ = msg.Term()
				} else {
					_ = msg.Nak()
				}
			}

			if msgs.Error() != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr,
					"comm.NATSBus: fetch error for agentID=%q: %v\n", agentID, msgs.Error())
			}
		}
	}()

	return func() {
		cancel()
		<-done
		// Best-effort delete of the durable consumer so re-Subscribe gets
		// a fresh sequence. In crash-recovery scenarios the consumer may
		// already be gone; ignore the error.
		delCtx, delCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer delCancel()
		_ = b.js.DeleteConsumer(delCtx, natsStreamName, durableName)

		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.subs {
			if s == sub {
				b.subs[i] = nil
				break
			}
		}
	}
}

// Publish marshals msg to JSON and publishes it to the per-recipient NATS
// subject. If msg.To is non-empty the message goes to the targeted agent's
// subject; if empty it goes to the broadcast subject.
//
// Publish also mirrors every non-broadcast message to the broadcast subject
// so that audit / observability subscribers (agentID == "") see the full
// message stream — matching the InMemoryBus semantic where broadcast
// subscribers receive all messages regardless of msg.To.
func (b *NATSBus) Publish(ctx context.Context, msg protocol.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "comm.NATSBus: marshal msg %s: %v\n", msg.ID, err)
		return
	}

	pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Deliver to the targeted subject (or broadcast if msg.To is empty).
	subj := b.subject(msg.To)
	if _, err := b.js.Publish(pubCtx, subj, data); err != nil {
		fmt.Fprintf(os.Stderr,
			"comm.NATSBus: publish msg %s to %s: %v\n", msg.ID, subj, err)
	}

	// Mirror to broadcast so observability sinks receive every message.
	// Only do this when the primary subject was NOT already the broadcast
	// subject (i.e. msg.To is non-empty) to avoid double-delivery to
	// broadcast subscribers.
	if msg.To != "" {
		bcastSubj := b.subject("")
		if _, err := b.js.Publish(pubCtx, bcastSubj, data); err != nil {
			fmt.Fprintf(os.Stderr,
				"comm.NATSBus: publish msg %s to broadcast: %v\n", msg.ID, err)
		}
	}
}

// deadLetter republishes an unprocessable message to the DLQ subject
// (bodh.{tenant}.dlq.{agentID}) with the failure reason in a header, for
// operator inspection. It is best-effort: a DLQ publish failure is logged but
// not retried, because the caller is about to Term the original to stop an
// unbounded redelivery loop — losing visibility on a poison message is the
// lesser harm versus wedging the consumer. The raw bytes are preserved so a
// human can see exactly what failed to parse or process.
func (b *NATSBus) deadLetter(agentID string, msg jetstream.Msg, reason string) {
	seg := strings.NewReplacer(" ", "_", "*", "_", ">", "_").Replace(agentID)
	if seg == "" {
		seg = natsBroadcastSegment
	}
	subj := fmt.Sprintf("%s.%s.%s.%s", natsSubjectPrefix, b.tenantID, natsDLQSegment, seg)

	hdr := nats.Header{}
	hdr.Set("X-Bodh-DLQ-Reason", reason)
	hdr.Set("X-Bodh-DLQ-Agent", agentID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := b.js.PublishMsg(ctx, &nats.Msg{Subject: subj, Data: msg.Data(), Header: hdr}); err != nil {
		fmt.Fprintf(os.Stderr,
			"comm.NATSBus: dead-letter publish for agentID=%q to %s: %v\n", agentID, subj, err)
	}
}

// Close drains all active subscriptions and closes the NATS connection.
// After Close the bus must not be used.
func (b *NATSBus) Close() error {
	b.mu.Lock()
	subs := make([]*natsSub, len(b.subs))
	copy(subs, b.subs)
	b.mu.Unlock()

	for _, s := range subs {
		if s != nil {
			s.cancel()
			<-s.done
		}
	}
	b.nc.Drain()
	return nil
}

// Compile-time assertion: NATSBus satisfies Bus.
var _ Bus = (*NATSBus)(nil)
