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

	// governance is imported only for its PolicyResult shape — this package
	// must not call governance evaluators directly; that would invert the
	// dependency. The orchestrator builds the Event, audit just records it.
	"github.com/PratikDhanave/bodh/pkg/governance"
	// protocol.Message is the only PHI-bearing payload we ever see; the
	// FromMessage helper is the chokepoint that strips it down to safe fields.
	"github.com/PratikDhanave/bodh/pkg/protocol"
	// google/uuid is used solely for Event.ID generation. UUIDv4 is sufficient
	// here: audit IDs are not security tokens, they are correlation handles
	// looked up by investigators alongside MessageID/CaseID.
	"github.com/google/uuid"
)

// Kind groups audit events for filtering. It is a string (not an int enum) so
// the JSON-encoded form on disk is human-readable and grep-able without a
// codebook — important when the audit log is the forensic record an
// investigator reaches for under HIPAA §164.404 breach-scope analysis.
type Kind string

// Audit event kinds. See docs/governance.md § Audit events / Hooks for the
// table of which hook in pkg/orchestration emits which kind. Adding a new
// Kind requires three things in lock-step: (1) a new const here, (2) a new
// emission site in the orchestrator (or in a gate agent for review-decision),
// and (3) a new branch in cmd/care/explain.go's switch on e.Kind — otherwise
// the right-to-explanation response silently drops the new category.
const (
	// KindMessage is emitted once per agent invocation, just before the
	// handler runs. This is the "agent saw a message" record — the spine of
	// the reasoning chain reconstructed by GET /explain/{case_id}.
	KindMessage Kind = "message"

	// KindPolicyDecision is emitted for every governance deny, and for
	// allows on clinical message types only. Non-clinical allows are skipped
	// on purpose: the log would otherwise drown in bookkeeping decisions
	// and the deny needles would be lost in the allow haystack.
	KindPolicyDecision Kind = "policy_decision"

	// KindReviewDecision is emitted by the HITL gate when a reviewer
	// approves / approves-with-modifications / rejects a queued ReviewRequest.
	// This is the regulatory anchor for 21 USC §360j(o)(1) CDS carve-out
	// criterion 4 — proof that a licensed clinician independently reviewed
	// the recommendation before any clinical action proceeded.
	KindReviewDecision Kind = "review_decision"

	// KindAgentError is emitted when an agent handler returns a non-nil err.
	// The error string is captured in Event.ErrorText (NOT msg.Content) so
	// the failure is reconstructible without copying any PHI the message
	// might have carried.
	KindAgentError Kind = "agent_error"
)

// Event is one row in the audit log. Field selection is deliberate: enough to
// reconstruct who did what when, never enough to leak PHI on its own. See
// docs/governance.md § Audit events for the "enough to reconstruct, never
// enough to leak" framing, and bodh-linkedin/06-audit-log-design.md for the
// design rationale write-up.
//
// What is DELIBERATELY ABSENT from this struct (and must stay absent):
//   - msg.Content — chief complaint / HPI / narrative — PHI
//   - any test result values or imaging findings — PHI
//   - any diagnosis or differential text — PHI
//   - any medication names or doses — PHI
//   - patient name, date of birth, MRN, address, phone, email — PHI
//
// The fields below are correlation handles only. They let an investigator
// pull the regulated data from a separately-controlled store under separate
// access control. If this log is ever compromised the worst case is leaked
// routing — never leaked clinical data. This is the project's expression of
// HIPAA's minimum-necessary principle (45 CFR §164.502(b)) applied to the
// audit pipeline itself.
type Event struct {
	// ID is a UUIDv4 generated per event. Used by investigators to cite a
	// single row when correlating against external logs / SIEM exports.
	// Backfilled by stamp() when callers leave it empty.
	ID string `json:"id"`

	// At is the event wall-clock time in UTC. Backfilled by stamp() when
	// callers leave it zero. UTC is mandatory because audit logs are
	// frequently aggregated cross-region; local time would scramble the
	// reconstruction order during DST transitions or multi-cell deployments.
	At time.Time `json:"at"`

	// Kind discriminates the row's meaning. See the const block above.
	Kind Kind `json:"kind"`

	// AgentID is the handler about to (or just) run. For policy decisions
	// this is the literal string "orchestrator" — the policy is not an
	// agent in the registry sense, but recording the source uniformly here
	// keeps downstream queries simple.
	AgentID string `json:"agent_id,omitempty"`

	// MessageID is the protocol.Message ID this event references. For HITL
	// review decisions it is overloaded to carry the ReviewRequest.ID
	// (the queue's own ID, which is the resume handle).
	MessageID string `json:"message_id,omitempty"`

	// From / To / Type are the routing triple from the protocol.Message
	// envelope. They reconstruct the message graph without exposing payload.
	// Type is e.g. "diagnosis_proposal" / "test_request" — the wire-level
	// message-type catalog, never a clinical free-text field.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	Type string `json:"type,omitempty"`

	// TenantID is the multi-tenant scoping correlation key. It is NEVER a
	// patient identifier; it is the operator's tenant/organization tag and
	// matches the PostgreSQL RLS row-level-security predicate (see
	// db/migrations/0002_rls.sql, wired by PR #10).
	TenantID string `json:"tenant_id,omitempty"`

	// PatientID is intentionally OPAQUE — a Bodh-internal opaque token
	// (e.g. "pt-001"), never the MRN. The MRN lives in the persistence
	// layer's PHI columns behind separate access control.
	PatientID string `json:"patient_id,omitempty"`

	// CaseID is the diagnostic case correlation token. It is opaque and is
	// the join key both GET /explain/{case_id} and the eval store use.
	CaseID string `json:"case_id,omitempty"`

	// TraceID propagates distributed-tracing correlation; sha256-hashed
	// case_id is used downstream in OTel spans (see PR #7) — raw MRN is
	// never attached to any span.
	TraceID string `json:"trace_id,omitempty"`

	// Decision is the policy verdict (allow / deny) for KindPolicyDecision,
	// or the reviewer's choice (approve / approve_with_modifications /
	// reject) for KindReviewDecision.
	Decision string `json:"decision,omitempty"`

	// Reason is the policy rationale or the reviewer's free-text rationale.
	// HITL UI guidance trains reviewers to keep this PHI-free; pkg/phi at
	// the logger seam is the defence-in-depth if a reviewer slips up.
	Reason string `json:"reason,omitempty"`

	// ReviewerID identifies the clinician who decided a HITL review. This
	// is the audit anchor for the 21 USC §360j(o)(1) carve-out criterion 4
	// — proof that a licensed reviewer was the one who approved.
	ReviewerID string `json:"reviewer_id,omitempty"`

	// ErrorText carries err.Error() for KindAgentError ONLY. It must never
	// be set from msg.Content. The orchestrator's recordAgentError helper
	// enforces this discipline (see pkg/orchestration/orchestrator.go).
	ErrorText string `json:"error,omitempty"`
}

// Recorder is what callers depend on. Implementations must be safe for
// concurrent calls because the orchestrator dispatches messages on bus
// goroutines (one per Subscribe handler) and every dispatch may call
// Record concurrently.
//
// Record returns an error, but callers in pkg/orchestration are expected to
// log-and-continue on failure — see orchestrator.recordAudit for the
// fail-open seam. The rationale: audit-sink unavailability (disk full,
// network partition to the WORM forwarder) is an operational problem; it
// is NEVER safer to halt clinical flow because the audit failed. Audit
// failure itself becomes an operational log line that can trigger paging.
type Recorder interface {
	// Record persists one Event. Implementations may block briefly but
	// must not block the calling goroutine for arbitrary durations — the
	// caller is on the message-dispatch hot path.
	Record(e Event) error
	// List returns a snapshot for inspection (tests, the /audit HTTP
	// endpoint, GET /explain/{case_id}). Production deployments query the
	// downstream SIEM rather than calling List() in a hot loop.
	List() []Event
}

// InMemory is a thread-safe slice-backed Recorder, intended for tests and
// demos. NOT suitable for production — restarts lose the log, which by
// itself violates HIPAA §164.312(b) ("record and examine activity"). The
// production path is FileStore → forwarder → Postgres audit_events table
// (append-only at the DB grants, see db/migrations/0002_rls.sql, PR #10)
// → hourly CronJob export → WORM sink (S3 Object Lock / Azure Immutable
// Blob) → 6-year retention with retention lock. See docs/governance.md
// § Production sink chain.
type InMemory struct {
	// mu protects events. sync.Mutex (not RWMutex) because Record is the
	// hot path; List is comparatively rare and tolerates a full lock.
	mu sync.Mutex
	// events is the in-process audit tape. Growth is unbounded by design;
	// process lifetime is the implicit retention bound for this variant.
	events []Event
}

// NewInMemory returns an empty in-memory recorder.
func NewInMemory() *InMemory { return &InMemory{} }

// Record appends an event. Fills in ID and At if missing.
//
// Backfilling ID/At inside Record (rather than at the call site) means the
// FromMessage / FromPolicyDecision / FromReviewDecision helpers can stay
// pure and time-source-free; the orchestrator can also construct Events
// inline without remembering to stamp them. Both paths converge here.
func (i *InMemory) Record(e Event) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	stamp(&e)
	i.events = append(i.events, e)
	return nil
}

// List returns a snapshot copy of every recorded event. The copy is
// mandatory: returning the underlying slice would let a caller hold a
// reference that another Record() could mutate from underneath them
// (append may grow the backing array, or it may not). Snapshot semantics
// are the contract.
func (i *InMemory) List() []Event {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]Event, len(i.events))
	copy(out, i.events)
	return out
}

// FileStore appends one JSON line per event to a file. Open it once at startup
// and reuse across the lifetime of the process; the underlying file handle
// stays open for write efficiency (one open+write+close per event would dwarf
// the JSON-encode cost on busy deployments).
//
// File format is JSONL (one Event per line). This is grep/jq-friendly and
// survives partial writes: a torn write leaves at most one corrupt line that
// json.Unmarshal will skip on read. Block-aligned binary formats (e.g.
// protobuf with length prefixes) would corrupt the tail of the file instead.
//
// This is appropriate as the first hop ONLY. Production must forward this
// file to a WORM (Write-Once-Read-Many) sink: S3 Object Lock or Azure
// Immutable Blob with a 6-year retention lock per HIPAA §164.530(j). The
// local file is NOT itself WORM — the OS can still truncate or unlink it,
// which would violate the §164.312(b) "record and examine activity"
// requirement on its own. See docs/governance.md § Production sink chain.
type FileStore struct {
	// mu serializes Write to keep JSONL line boundaries intact under
	// concurrent Record calls. Without this, two goroutines could interleave
	// JSON bytes and produce un-parseable lines.
	mu sync.Mutex
	// path is retained so List() can re-open the file for read; the write
	// handle is not seekable in O_APPEND mode on all platforms.
	path string
	// f is the long-lived append-only write handle. Closed only by Close().
	f *os.File
}

// OpenFile opens (creates) the audit log at path. The file is opened
// append-only; rotation is out of scope and handled by the operator (logrotate,
// fluentbit, vector, etc.) by writing a fresh file under the same path.
//
// File mode 0o600 (owner read/write only) is mandatory: the audit log
// contains tenant IDs and case IDs that, while not PHI on their own, are
// sensitive correlation handles. Group / other access would violate HIPAA
// §164.312(a)(1) access control on the audit artefact itself.
func OpenFile(path string) (*FileStore, error) {
	if path == "" {
		// Reject empty path early; otherwise os.OpenFile produces a
		// cryptic error that callers misdiagnose as "audit disabled".
		return nil, errors.New("audit: empty path")
	}
	// O_APPEND is the integrity primitive: every write atomically lands at
	// the current EOF, even under concurrent processes (POSIX guarantee for
	// writes ≤ PIPE_BUF; we serialize via mu anyway for safety on large
	// events). O_CREATE means first-run is hands-off for the operator.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	return &FileStore{path: path, f: f}, nil
}

// Record appends one JSON-encoded line for the event.
//
// Two design notes:
//  1. The write is NOT fsync'd here. fsync per event would cap throughput at
//     a few hundred events/sec on typical SSDs. The trade-off accepted: on
//     a crash, a tail of unflushed events is lost. The downstream forwarder
//     (fluentbit / vector with at-least-once delivery) is what makes the
//     pipeline durable; this file is only a buffer.
//  2. The append+newline is two pieces of work (Marshal, then Write). The
//     newline is appended to the JSON bytes BEFORE the single Write call,
//     so the line is one syscall — eliminating the only window where a
//     concurrent process could see a half-written line.
func (s *FileStore) Record(e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stamp(&e)
	b, err := json.Marshal(e)
	if err != nil {
		// Marshal failure typically means a non-UTF-8 byte slipped into a
		// string field. Return the error; the orchestrator will log it.
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
//
// We Sync() first so any buffered bytes are visible to the fresh os.Open
// handle below; otherwise tests immediately following a Record could miss
// the event on read. Sync failure returns nil (not an error) because List
// is best-effort by contract; cmd/care's /audit endpoint handles a nil
// return as "no events visible right now".
//
// Lines that fail json.Unmarshal are silently dropped. This is the right
// trade-off for an audit reader: a single corrupt tail (from a power-loss
// torn write) must not prevent reconstructing the rest of the log.
func (s *FileStore) List() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.f.Sync(); err != nil {
		return nil
	}
	// Open a fresh read handle; the write handle is opened O_WRONLY.
	f, err := os.Open(s.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Event
	scanner := bufio.NewScanner(f)
	// 1 MiB max line. Audit events are tiny (a few hundred bytes), but the
	// default 64 KiB ceiling is too low if a future field carries a long
	// reviewer rationale; bumping it here keeps the file readable without
	// needing a code change later.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err == nil {
			out = append(out, e)
		}
	}
	return out
}

// Close flushes and closes the underlying file. Safe to call multiple times
// (subsequent calls become no-ops). Callers should defer this from main()
// so a clean shutdown flushes any kernel buffer; an os.Exit or SIGKILL will
// bypass it, but those paths are expected to lose the very tail anyway.
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	// Zero out to make double-Close safe and to surface nil-deref bugs
	// loudly if anyone uses the FileStore after Close.
	s.f = nil
	return err
}

// FromMessage builds a KindMessage event from a Message. The agentID is the
// handler that's about to (or just did) run.
//
// CRITICAL CONTRACT: this helper copies routing fields (ID, From, To, Type)
// and the four metadata correlation keys, and NOTHING ELSE. In particular,
// msg.Content is never touched. The PHI guarantee of the audit log
// depends on this — any future field added here must be reviewed against
// the "enough to reconstruct, never enough to leak" rule.
func FromMessage(agentID string, msg protocol.Message) Event {
	e := Event{
		Kind:      KindMessage,
		AgentID:   agentID,
		MessageID: msg.ID,
		From:      msg.From,
		To:        msg.To,
		Type:      msg.Type,
		// NOTE: msg.Content (clinical narrative / patient data) is intentionally NOT copied.
	}
	// pickStrings copies only the four named correlation keys from metadata,
	// not arbitrary map contents — see its body for the allowlist.
	pickStrings(msg.Metadata, &e)
	return e
}

// FromPolicyDecision builds a KindPolicyDecision event for one governance hop.
//
// AgentID is hard-coded to "orchestrator" because policy evaluation runs in
// the orchestrator boundary (see pkg/orchestration/orchestrator.go), not in
// any registered agent. Keeping the field non-empty simplifies downstream
// filters that group by AgentID.
//
// res.Reason is copied verbatim — policies are responsible for keeping
// their reason strings PHI-free (the medical policy in
// pkg/governance/medical_policy.go follows this convention by emitting
// template strings like "case_id required" rather than echoing payload).
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
//
// MessageID is overloaded to carry the ReviewRequest.ID (the queue's own ID)
// because review decisions don't have a protocol.Message of their own — the
// gated message is upstream, the resume message is downstream. Using the
// review ID here makes the audit row joinable to the queue lookup.
//
// PatientID and CaseID are passed explicitly because the gate agent (not
// the orchestrator's policy hop) constructs this Event, and it has the
// review's correlation context in hand without needing to re-parse a
// protocol.Message envelope.
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

// pickStrings copies the four whitelisted correlation keys from a message's
// metadata into the Event. The whitelist is deliberate: protocol.Message
// metadata is an open map[string]any and could in principle carry anything
// a caller stuffs into it (chief_complaint, hl7_segment, etc.). Copying
// arbitrary keys would defeat the PHI guarantee of the audit schema.
//
// Adding a new key here is a governance decision — it MUST be a correlation
// handle, never a payload field. See docs/governance.md § Audit events.
func pickStrings(m map[string]any, e *Event) {
	if m == nil {
		return
	}
	// patient_id is opaque (e.g. "pt-001"), not the MRN.
	if v, ok := m["patient_id"].(string); ok {
		e.PatientID = v
	}
	// case_id is opaque; same value GET /explain/{case_id} joins on.
	if v, ok := m["case_id"].(string); ok {
		e.CaseID = v
	}
	// trace_id is the distributed-tracing correlation key (OTel-compatible).
	if v, ok := m["trace_id"].(string); ok {
		e.TraceID = v
	}
	// tenant_id is the multi-tenant scope key matching Postgres RLS.
	if v, ok := m["tenant_id"].(string); ok {
		e.TenantID = v
	}
}

// stamp backfills ID and At so the FromX helpers can stay free of clock
// and uuid dependencies. Records that already carry ID/At are left alone
// — useful for tests that need deterministic timestamps and for any future
// replay pipeline that re-ingests historical events without rewriting their
// IDs (which would break investigator citations).
func stamp(e *Event) {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.At.IsZero() {
		// UTC is mandatory; see Event.At doc.
		e.At = time.Now().UTC()
	}
}
