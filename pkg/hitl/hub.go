// Package hitl — WebSocket broadcast hub for real-time HITL notifications.
//
// Clinicians connect to GET /ws/hitl?tenant_id=<id> and receive JSON-encoded
// Event messages whenever a ReviewRequest is submitted or decided.  The
// transport is a minimal hand-rolled WebSocket (RFC 6455 subset) so the
// package remains stdlib-only and the binary stays free of gorilla/websocket.
//
// Frame encoding note: only TEXT frames (opcode 0x1) are written; all frames
// are unmasked (server→client direction).  Ping frames (opcode 0x9) are sent
// by the server every 30 s to keep the connection alive through proxies; the
// hub does not parse incoming frames from the client (browser sends masked
// pong automatically), so only the connection liveness matters.
package hitl

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// Event is the JSON payload pushed to every connected clinician when something
// notable happens in the HITL queue.
type Event struct {
	// Type identifies what happened: "review_submitted", "review_decided", or
	// "ping" (keepalive).
	Type string `json:"type"`
	// Payload carries the ReviewRequest (on submit) or DecisionEvent (on
	// decide) as raw JSON so callers can unmarshal into the concrete type.
	// Nil on ping events.
	Payload json.RawMessage `json:"payload,omitempty"`
	// At is an RFC 3339 timestamp — UTC, nanosecond precision.
	At string `json:"at"`
}

// DecisionEvent is the Payload for a "review_decided" Event.
type DecisionEvent struct {
	ReviewID string `json:"review_id"`
	Decision string `json:"decision"` // "approve" | "reject" | "approve_with_modifications"
	By       string `json:"by"`
}

// chanSet is a set of event channels for one tenant.
type chanSet = map[chan Event]struct{}

// Hub multiplexes events to all connected WebSocket clients, scoped by tenant.
//
// Clients subscribe via Subscribe (internal / test use) or via ServeHTTP which
// accepts an HTTP Upgrade and drives the WebSocket lifecycle.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]chanSet // tenant_id → set of channels
	queue   *Queue
}

// NewHub builds a Hub wired to the supplied queue.  The queue reference is
// used only by NotifySubmit (called from Queue.Submit when Hub is set).
func NewHub(q *Queue) *Hub {
	return &Hub{
		clients: make(map[string]chanSet),
		queue:   q,
	}
}

// Subscribe registers a new channel for the given tenant.  The returned
// channel is buffered (64 events) so a slow consumer does not block the
// broadcaster.  Calling unsub removes the channel from the hub and closes it.
//
// Subscribe is the internal/test-facing API; production clients arrive via
// ServeHTTP.
func (h *Hub) Subscribe(tenantID string) (ch <-chan Event, unsub func()) {
	c := make(chan Event, 64)
	h.mu.Lock()
	if h.clients[tenantID] == nil {
		h.clients[tenantID] = make(chanSet)
	}
	h.clients[tenantID][c] = struct{}{}
	h.mu.Unlock()

	return c, func() {
		h.mu.Lock()
		delete(h.clients[tenantID], c)
		if len(h.clients[tenantID]) == 0 {
			delete(h.clients, tenantID)
		}
		h.mu.Unlock()
		// Drain + close so any goroutine blocked on <-ch unblocks.
		close(c)
	}
}

// Broadcast sends ev to every subscriber for tenantID.  Channels that are
// full (slow consumer) are skipped to avoid blocking the caller — a missed
// keepalive is acceptable; a missed review notification will be delivered on
// the next submit/decide or discovered on the next UI poll.
func (h *Hub) Broadcast(tenantID string, ev Event) {
	// Hold the read lock across the whole loop: `set` is the same map object
	// stored in h.clients, and the unsubscribe closure deletes from it and
	// closes the channel under the write lock. Ranging/sending without the lock
	// races with that (concurrent map iteration + write, or send on a closed
	// channel). The non-blocking select keeps the critical section short.
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients[tenantID] {
		select {
		case c <- ev:
		default:
			// Slow consumer: skip rather than block.
		}
	}
}

// NotifySubmit broadcasts a "review_submitted" event to tenantID subscribers.
func (h *Hub) NotifySubmit(tenantID string, r medical.ReviewRequest) {
	payload, _ := json.Marshal(r)
	h.Broadcast(tenantID, Event{
		Type:    "review_submitted",
		Payload: json.RawMessage(payload),
		At:      time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// NotifyDecide broadcasts a "review_decided" event to tenantID subscribers.
func (h *Hub) NotifyDecide(tenantID, reviewID, decision, by string) {
	payload, _ := json.Marshal(DecisionEvent{
		ReviewID: reviewID,
		Decision: decision,
		By:       by,
	})
	h.Broadcast(tenantID, Event{
		Type:    "review_decided",
		Payload: json.RawMessage(payload),
		At:      time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// wsGUID is the magic GUID from RFC 6455 § 1.3.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// ServeHTTP implements http.Handler for the WebSocket upgrade endpoint.
//
// Required query parameter: ?tenant_id=<id>  — absent/empty → 400.
// Required headers: Upgrade: websocket, Connection: Upgrade,
// Sec-WebSocket-Key: <base64>.
//
// The handler:
//  1. Validates the upgrade request and replies 101.
//  2. Subscribes to tenant events.
//  3. Sends a keepalive ping every 30 s.
//  4. Forwards events from the subscription channel as text frames.
//  5. Returns (closing the connection) when the client disconnects.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id query parameter required", http.StatusBadRequest)
		return
	}

	// Validate WebSocket upgrade headers.
	if r.Header.Get("Upgrade") != "websocket" {
		http.Error(w, "Upgrade: websocket required", http.StatusBadRequest)
		return
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "Sec-WebSocket-Key required", http.StatusBadRequest)
		return
	}

	// Compute accept token: SHA-1(key + GUID), base64.
	h1 := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(h1[:])

	// Hijack the connection before writing the 101 response so we control the
	// raw TCP stream for WebSocket framing.
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "server does not support hijacking", http.StatusInternalServerError)
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	// Write the 101 Switching Protocols response directly over the raw connection.
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n" +
		"\r\n"
	if _, err := bufrw.WriteString(resp); err != nil {
		return
	}
	if err := bufrw.Flush(); err != nil {
		return
	}

	// Subscribe to tenant events.
	ch, unsub := h.Subscribe(tenantID)
	defer unsub()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case ev, open := <-ch:
			if !open {
				// Hub closed the channel (shutdown path).
				return
			}
			data, _ := json.Marshal(ev)
			if err := writeTextFrame(bufrw, data); err != nil {
				return
			}
		case <-ticker.C:
			// Send a WebSocket ping frame (opcode 0x9) as keepalive.
			pingMsg, _ := json.Marshal(Event{
				Type: "ping",
				At:   time.Now().UTC().Format(time.RFC3339Nano),
			})
			// We send it as a text frame carrying the ping JSON so browser
			// clients that only handle message events still see it.
			if err := writeTextFrame(bufrw, pingMsg); err != nil {
				return
			}
		}
	}
}

// writeTextFrame writes a single RFC 6455 text (opcode 0x1) frame with FIN=1
// and no masking (server→client frames are never masked per RFC 6455 §5.1).
//
// Frame layout (simplified — only the cases we need):
//
//	Byte 0: FIN(1) | RSV(000) | opcode(0001 = text)  → 0x81
//	Byte 1: MASK(0) | payload_len_7bit
//	  if payload_len == 126: bytes 2-3 = uint16 extended length
//	  if payload_len == 127: bytes 2-9 = uint64 extended length
//	Payload bytes (unmasked).
//
// We only handle payloads ≤ 65535 bytes. Clinical events are well within that
// limit; anything larger is a programming error and the write fails silently.
type flusher interface {
	Write([]byte) (int, error)
	Flush() error
}

func writeTextFrame(bw flusher, payload []byte) error {
	n := len(payload)
	var header []byte
	switch {
	case n <= 125:
		header = []byte{0x81, byte(n)}
	case n <= 65535:
		header = []byte{0x81, 126, byte(n >> 8), byte(n)}
	default:
		// Payload too large — skip rather than corrupt the stream.
		return nil
	}
	if _, err := bw.Write(header); err != nil {
		return err
	}
	if _, err := bw.Write(payload); err != nil {
		return err
	}
	return bw.Flush()
}
