// Package webhooks provides push-notification delivery to external systems
// when Bodh platform events occur (diagnosis complete, review submitted, etc.).
//
// Each tenant registers one or more Endpoint records. When Dispatch is called
// the Dispatcher fans out concurrently to every active endpoint that has
// subscribed to the fired EventType, signs the JSON payload with
// HMAC-SHA256 (X-Bodh-Signature: sha256=<hex>), and retries up to three times
// on transient failure (1 s → 5 s → 15 s back-off). Delivery is fire-and-forget
// — the caller is never blocked.
package webhooks

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EventType identifies the Bodh platform event that triggered a webhook.
type EventType string

const (
	EventDiagnosisComplete EventType = "diagnosis_complete"
	EventReviewSubmitted   EventType = "review_submitted"
	EventReviewDecided     EventType = "review_decided"
	EventAlertTriggered    EventType = "alert_triggered"
	EventPanelGap          EventType = "panel_gap_found"
)

// Endpoint is a subscriber record for a single external URL. Secret is used
// to compute the HMAC-SHA256 signature on every delivery so the receiver can
// verify authenticity without sharing a transport-level credential.
type Endpoint struct {
	ID       string
	TenantID string
	URL      string
	Secret   string // HMAC-SHA256 signing secret
	Events   []EventType
	Active   bool
	Created  time.Time
}

// WebhookPayload is the JSON envelope posted to each endpoint.
type WebhookPayload struct {
	ID        string    `json:"id"` // uuid
	Event     EventType `json:"event"`
	TenantID  string    `json:"tenant_id"`
	Data      any       `json:"data"`
	Timestamp string    `json:"timestamp"`
}

// Dispatcher manages tenant endpoint registrations and drives concurrent,
// signed, retrying HTTP delivery.
type Dispatcher struct {
	endpoints map[string][]Endpoint // tenant_id → []Endpoint
	mu        sync.RWMutex
	client    *http.Client
}

// NewDispatcher returns a ready-to-use Dispatcher with a 10-second per-attempt
// HTTP client timeout.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		endpoints: make(map[string][]Endpoint),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Register adds ep to the tenant's endpoint list, assigning a new UUID ID if
// ep.ID is empty. It returns the (possibly newly assigned) endpoint ID.
func (d *Dispatcher) Register(ep Endpoint) string {
	if ep.ID == "" {
		ep.ID = uuid.New().String()
	}
	if ep.Created.IsZero() {
		ep.Created = time.Now().UTC()
	}
	ep.Active = true

	d.mu.Lock()
	defer d.mu.Unlock()
	d.endpoints[ep.TenantID] = append(d.endpoints[ep.TenantID], ep)
	return ep.ID
}

// Deregister removes the endpoint with the given ID from tenantID's list.
// It is a no-op if the endpoint is not found.
func (d *Dispatcher) Deregister(tenantID, epID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	eps := d.endpoints[tenantID]
	out := eps[:0]
	for _, ep := range eps {
		if ep.ID != epID {
			out = append(out, ep)
		}
	}
	d.endpoints[tenantID] = out
}

// List returns a snapshot of all endpoints registered for tenantID.
func (d *Dispatcher) List(tenantID string) []Endpoint {
	d.mu.RLock()
	defer d.mu.RUnlock()
	src := d.endpoints[tenantID]
	out := make([]Endpoint, len(src))
	copy(out, src)
	return out
}

// Dispatch fires the event to every active endpoint of tenantID that has
// subscribed to the given EventType. Each delivery runs in its own goroutine
// (fire-and-forget); the caller returns immediately. Delivery is signed and
// retried up to three times with 1 s, 5 s, 15 s back-off.
func (d *Dispatcher) Dispatch(tenantID string, event EventType, data any) {
	d.mu.RLock()
	eps := make([]Endpoint, 0, len(d.endpoints[tenantID]))
	for _, ep := range d.endpoints[tenantID] {
		if !ep.Active {
			continue
		}
		for _, e := range ep.Events {
			if e == event {
				eps = append(eps, ep)
				break
			}
		}
	}
	d.mu.RUnlock()

	if len(eps) == 0 {
		return
	}

	payload := WebhookPayload{
		ID:        uuid.New().String(),
		Event:     event,
		TenantID:  tenantID,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for _, ep := range eps {
		ep := ep // capture loop variable
		go d.deliver(ep, body)
	}
}

// deliver posts body to ep.URL, retrying on non-2xx or transport error.
// Back-off intervals: 1 s, 5 s, 15 s.
func (d *Dispatcher) deliver(ep Endpoint, body []byte) {
	backoff := []time.Duration{time.Second, 5 * time.Second, 15 * time.Second}

	sig := sign(ep.Secret, body)

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff[attempt-1])
		}
		if err := d.post(ep.URL, sig, body); err == nil {
			return
		}
	}
	// All three attempts exhausted — drop silently (fire-and-forget contract).
}

// post performs a single HTTP POST and returns an error on non-2xx status or
// transport failure.
func (d *Dispatcher) post(url, sig string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bodh-Signature", "sha256="+sig)

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: non-2xx status %d from %s", resp.StatusCode, url)
	}
	return nil
}

// sign returns the lowercase hex HMAC-SHA256 of body keyed by secret.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
