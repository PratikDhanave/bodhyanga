// Package engagement is a tiny outreach notifier used by the engagement agent.
// It logs SMS / email / IVR messages to memory so demos and tests can assert
// what was "sent" without hitting any external service.
package engagement

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Channel is the delivery mechanism.
type Channel string

const (
	ChannelSMS   Channel = "sms"
	ChannelEmail Channel = "email"
	ChannelIVR   Channel = "ivr"
)

// Notification represents one outreach attempt.
type Notification struct {
	ID        string    `json:"id"`
	PatientID string    `json:"patient_id"`
	Channel   Channel   `json:"channel"`
	Template  string    `json:"template"`
	Body      string    `json:"body"`
	Reason    string    `json:"reason,omitempty"`
	CaseID    string    `json:"case_id,omitempty"`
	SentAt    time.Time `json:"sent_at"`
	Acked     bool      `json:"acked,omitempty"`
	AckedAt   time.Time `json:"acked_at,omitempty"`
}

// Notifier is the abstraction. In production this fans out to Twilio / SendGrid.
type Notifier interface {
	Send(n Notification) Notification
	List() []Notification
	Ack(id string) (Notification, bool)
}

// InMemoryNotifier records notifications and supports ack tracking
// for an adherence rate.
type InMemoryNotifier struct {
	mu    sync.RWMutex
	items map[string]Notification
	order []string
}

// NewInMemory returns an empty notifier.
func NewInMemory() *InMemoryNotifier {
	return &InMemoryNotifier{items: map[string]Notification{}}
}

// Send stamps + stores the notification.
func (n *InMemoryNotifier) Send(item Notification) Notification {
	n.mu.Lock()
	defer n.mu.Unlock()
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.SentAt.IsZero() {
		item.SentAt = time.Now().UTC()
	}
	n.items[item.ID] = item
	n.order = append(n.order, item.ID)
	return item
}

// Ack marks a notification as received/acted upon.
func (n *InMemoryNotifier) Ack(id string) (Notification, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	item, ok := n.items[id]
	if !ok {
		return Notification{}, false
	}
	item.Acked = true
	item.AckedAt = time.Now().UTC()
	n.items[id] = item
	return item, true
}

// List returns notifications in send order.
func (n *InMemoryNotifier) List() []Notification {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make([]Notification, 0, len(n.order))
	for _, id := range n.order {
		if item, ok := n.items[id]; ok {
			out = append(out, item)
		}
	}
	return out
}

// AdherenceRate returns the fraction of notifications acked so far.
func (n *InMemoryNotifier) AdherenceRate() float64 {
	items := n.List()
	if len(items) == 0 {
		return 0
	}
	acked := 0
	for _, it := range items {
		if it.Acked {
			acked++
		}
	}
	return float64(acked) / float64(len(items))
}

// FormatGapMessage produces the SMS/email body for a care-gap outreach.
func FormatGapMessage(patientID, rule, detail string) string {
	return fmt.Sprintf("Hi %s — your care team flagged: %s. Reply 1 to schedule. (%s)", patientID, detail, rule)
}
