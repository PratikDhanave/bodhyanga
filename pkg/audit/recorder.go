// Package audit is the immutable, append-only audit log Bodh writes for every
// message, governance decision, and HITL review decision.
//
// HIPAA §164.312(b) requires "hardware, software, and procedural mechanisms
// that record and examine activity in information systems that contain or use
// electronic protected health information." 21 CFR Part 11 expects the same
// for FDA-regulated trial deployments. This package is the seam where that
// log is produced; the FileStore implementation appends one JSON line per
// event to a file (rotate / forward to a WORM sink in production).
package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/protocol"
	"github.com/google/uuid"
)

// Kind groups audit events for filtering.
type Kind string

const (
	KindMessage        Kind = "message"
	KindPolicyDecision Kind = "policy_decision"
	KindReviewDecision Kind = "review_decision"
	KindAgentError     Kind = "agent_error"
)

// Event is one row in the audit log. Field selection is deliberate: enough to
// reconstruct who did what when, never enough to leak PHI on its own.
type Event struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	Kind       Kind      `json:"kind"`
	AgentID    string    `json:"agent_id,omitempty"`
	MessageID  string    `json:"message_id,omitempty"`
	From       string    `json:"from,omitempty"`
	To         string    `json:"to,omitempty"`
	Type       string    `json:"type,omitempty"`
	TenantID   string    `json:"tenant_id,omitempty"`
	PatientID  string    `json:"patient_id,omitempty"`
	CaseID     string    `json:"case_id,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
	Decision   string    `json:"decision,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	ReviewerID string    `json:"reviewer_id,omitempty"`
	ErrorText  string    `json:"error,omitempty"`
}

// Recorder is what callers depend on. Implementations must be safe for
// concurrent calls.
type Recorder interface {
	Record(e Event) error
	List() []Event
}

// InMemory is a thread-safe slice-backed Recorder, intended for tests and
// demos. NOT suitable for production — restarts lose the log.
type InMemory struct {
	mu     sync.Mutex
	events []Event
}

// NewInMemory returns an empty in-memory recorder.
func NewInMemory() *InMemory { return &InMemory{} }

// Record appends an event. Fills in ID and At if missing.
func (i *InMemory) Record(e Event) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	stamp(&e)
	i.events = append(i.events, e)
	return nil
}

// List returns a snapshot copy of every recorded event.
func (i *InMemory) List() []Event {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]Event, len(i.events))
	copy(out, i.events)
	return out
}

// FileStore appends one JSON line per event to a file. Open it once at startup
// and reuse across the lifetime of the process; the underlying file handle
// stays open for write efficiency.
type FileStore struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

// OpenFile opens (creates) the audit log at path. The file is opened
// append-only; rotation is out of scope and handled by the operator (logrotate,
// fluentbit, etc.) by writing a fresh file under the same path.
func OpenFile(path string) (*FileStore, error) {
	if path == "" {
		return nil, errors.New("audit: empty path")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	return &FileStore{path: path, f: f}, nil
}

// Record appends one JSON-encoded line for the event.
func (s *FileStore) Record(e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stamp(&e)
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := s.f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// List rewinds the underlying file and parses every line. O(file size); use
// for inspection / tests, not for hot-path queries (in production query the
// downstream log/SIEM, not this file).
func (s *FileStore) List() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.f.Sync(); err != nil {
		return nil
	}
	f, err := os.Open(s.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err == nil {
			out = append(out, e)
		}
	}
	return out
}

// Close flushes and closes the underlying file.
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}

// FromMessage builds a KindMessage event from a Message. The agentID is the
// handler that's about to (or just did) run.
func FromMessage(agentID string, msg protocol.Message) Event {
	e := Event{
		Kind:      KindMessage,
		AgentID:   agentID,
		MessageID: msg.ID,
		From:      msg.From,
		To:        msg.To,
		Type:      msg.Type,
	}
	pickStrings(msg.Metadata, &e)
	return e
}

// FromPolicyDecision builds a KindPolicyDecision event for one governance hop.
func FromPolicyDecision(msg protocol.Message, res governance.PolicyResult) Event {
	e := Event{
		Kind:      KindPolicyDecision,
		AgentID:   "orchestrator",
		MessageID: msg.ID,
		From:      msg.From,
		To:        msg.To,
		Type:      msg.Type,
		Decision:  string(res.Decision),
		Reason:    res.Reason,
	}
	pickStrings(msg.Metadata, &e)
	return e
}

// FromReviewDecision builds a KindReviewDecision event for a HITL gate close-out.
func FromReviewDecision(reviewID, reviewerID, decision, reason, patientID, caseID string) Event {
	return Event{
		Kind:       KindReviewDecision,
		MessageID:  reviewID,
		ReviewerID: reviewerID,
		Decision:   decision,
		Reason:     reason,
		PatientID:  patientID,
		CaseID:     caseID,
	}
}

func pickStrings(m map[string]any, e *Event) {
	if m == nil {
		return
	}
	if v, ok := m["patient_id"].(string); ok {
		e.PatientID = v
	}
	if v, ok := m["case_id"].(string); ok {
		e.CaseID = v
	}
	if v, ok := m["trace_id"].(string); ok {
		e.TraceID = v
	}
	if v, ok := m["tenant_id"].(string); ok {
		e.TenantID = v
	}
}

func stamp(e *Event) {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
}
