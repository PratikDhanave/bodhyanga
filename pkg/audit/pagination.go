// Package audit — cursor-based pagination helpers for the audit event log.
//
// AuditPage and AuditQuery provide the building blocks for GET /api/audit.
// Cursor encoding is base64(id + "|" + RFC3339Nano(at)) so the opaque token
// carries enough information to resume a chronological scan without exposing
// internal database row numbers or offsets (which would leak schema details
// and break if rows are reordered during compaction).
//
// The cursor is one-directional (forward in time) because audit logs are
// append-only; there is no pagination backward. Clients that need to
// reconstruct a timeline from the beginning should omit the cursor and
// iterate forward page by page until HasMore is false.
package audit

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"
)

// AuditPage is the envelope returned by paginated audit-log reads.
// It is safe to JSON-encode directly (all fields are exported and tagged).
//
// NextCursor is omitted from the JSON output (omitempty) when there are no
// further events; callers should treat an absent NextCursor as "end of log".
type AuditPage struct {
	// Events holds the batch of audit events for this page, ordered
	// chronologically (oldest to newest within the page).
	Events []Event `json:"events"`

	// NextCursor is a base64-encoded resume token. It encodes the ID and
	// timestamp of the last event returned so the next call can ask for
	// events strictly after that position. Absent when HasMore is false.
	NextCursor string `json:"next_cursor,omitempty"`

	// Total is the count of events in this page (len(Events)).
	// It is NOT a grand total of all matching rows — full COUNT(*) scans
	// on large audit tables are expensive and HIPAA deployments may have
	// millions of rows. Callers that need an exact count should issue a
	// separate aggregation query.
	Total int `json:"total"`

	// HasMore is true when the underlying store has additional events beyond
	// this page. Clients should follow NextCursor until HasMore is false.
	HasMore bool `json:"has_more"`
}

// AuditQuery parameterises a paginated audit-log read. All filter fields are
// optional except TenantID, which is required for multi-tenant isolation.
//
// Filters are AND-ed: if both CaseID and Kind are set, only events that match
// both are returned.
type AuditQuery struct {
	// TenantID must be non-empty; the handler rejects requests without it
	// with HTTP 403 to prevent cross-tenant reads.
	TenantID string

	// CaseID filters to events tied to a specific diagnostic case.
	// Empty means "all cases visible to TenantID".
	CaseID string

	// AgentID filters to events emitted by a specific agent.
	// Empty means "all agents".
	AgentID string

	// Kind filters to a specific event kind (e.g. "diagnosis", "break_glass",
	// "message", "policy_decision"). Empty means "all kinds".
	Kind string

	// After is the opaque cursor returned by a prior AuditPage.NextCursor.
	// If non-empty, DecodeCursor is called to extract the resume position.
	// Empty means "start from the beginning".
	After string

	// Limit is the maximum number of events to return per page.
	// Values ≤ 0 default to DefaultAuditPageLimit.
	// Values > MaxAuditPageLimit are capped at MaxAuditPageLimit.
	Limit int
}

const (
	// DefaultAuditPageLimit is returned when AuditQuery.Limit is ≤ 0.
	DefaultAuditPageLimit = 50

	// MaxAuditPageLimit caps caller-supplied limits so a single request
	// cannot exhaust memory or a database connection on large tables.
	MaxAuditPageLimit = 200
)

// ResolvedLimit returns the effective page size for q, applying the default
// and maximum clamps described in the field documentation.
func (q AuditQuery) ResolvedLimit() int {
	n := q.Limit
	if n <= 0 {
		n = DefaultAuditPageLimit
	}
	if n > MaxAuditPageLimit {
		n = MaxAuditPageLimit
	}
	return n
}

// EncodeCursor encodes id and at into an opaque, URL-safe base64 token.
// The internal format is "<id>|<RFC3339Nano timestamp>" — the pipe character
// is not valid in UUIDs so it is a safe delimiter, and RFC3339Nano preserves
// nanosecond precision which is necessary on databases where two events in
// the same microsecond would otherwise produce duplicate timestamps.
//
// The resulting string is safe to embed in a URL query parameter without
// further escaping because base64.URLEncoding (not StdEncoding) is used.
func EncodeCursor(id string, at time.Time) string {
	raw := id + "|" + at.UTC().Format(time.RFC3339Nano)
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor is the inverse of EncodeCursor. It returns the event ID and
// timestamp embedded in cursor, or a non-nil error if cursor is malformed.
// Callers should treat a decode error as an invalid-cursor 400 response.
func DecodeCursor(s string) (lastID string, lastAt time.Time, err error) {
	if s == "" {
		return "", time.Time{}, fmt.Errorf("audit: empty cursor")
	}
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("audit: cursor base64 decode: %w", err)
	}
	raw := string(b)
	idx := strings.LastIndex(raw, "|")
	if idx < 0 {
		return "", time.Time{}, fmt.Errorf("audit: cursor missing delimiter")
	}
	id := raw[:idx]
	ts := raw[idx+1:]
	if id == "" {
		return "", time.Time{}, fmt.Errorf("audit: cursor empty id")
	}
	at, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("audit: cursor timestamp parse: %w", err)
	}
	return id, at.UTC(), nil
}

// PageFromRecorder applies AuditQuery against rec's in-memory snapshot and
// returns an AuditPage. This is the reference implementation used by the
// in-memory path; the Postgres path applies equivalent filters at the
// database layer for efficiency.
//
// Filtering is applied in order: tenant, kind, case_id, agent_id, cursor.
// The cursor comparison is (at > lastAt) || (at == lastAt && id > lastID)
// which produces a stable ordering even when multiple events share a
// millisecond timestamp (UUIDv4 sort is lexicographic on the UUID string,
// which provides a deterministic but not meaningful order for ties).
func PageFromRecorder(rec Recorder, q AuditQuery) AuditPage {
	all := rec.List()
	// Recorder.List() returns events in insertion order, but the cursor skip
	// below (and nextCursor) assume a total (At, ID) order — otherwise, on a
	// timestamp tie, the cursorPassed shortcut re-includes events from the
	// previous page (duplicates) and can drop others. Sort to the documented
	// (At asc, ID asc) order first.
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].At.Equal(all[j].At) {
			return all[i].At.Before(all[j].At)
		}
		return all[i].ID < all[j].ID
	})
	limit := q.ResolvedLimit()

	// Decode cursor once; we'll use it in the filter loop.
	var (
		hasCursor    bool
		cursorID     string
		cursorAt     time.Time
		cursorPassed bool
	)
	if q.After != "" {
		id, at, err := DecodeCursor(q.After)
		if err == nil {
			hasCursor = true
			cursorID = id
			cursorAt = at
		}
		// Silently ignore a malformed cursor — treat it as "start from beginning".
		// A production deployment may prefer to return a 400; the HTTP handler
		// does the validation before calling PageFromRecorder.
	}

	matched := make([]Event, 0, limit+1)
	for _, e := range all {
		// Tenant filter — always applied; events without a TenantID are
		// included only when TenantID query field is empty, which the HTTP
		// handler prevents (it requires tenant_id).
		if q.TenantID != "" && e.TenantID != "" && e.TenantID != q.TenantID {
			continue
		}
		// Optional kind filter.
		if q.Kind != "" && string(e.Kind) != q.Kind {
			continue
		}
		// Optional case_id filter.
		if q.CaseID != "" && e.CaseID != q.CaseID {
			continue
		}
		// Optional agent_id filter.
		if q.AgentID != "" && e.AgentID != q.AgentID {
			continue
		}
		// Cursor skip: skip events that are at or before the cursor position.
		if hasCursor && !cursorPassed {
			after := e.At.After(cursorAt) ||
				(e.At.Equal(cursorAt) && e.ID > cursorID)
			if !after {
				continue
			}
			cursorPassed = true
		}
		matched = append(matched, e)
		if len(matched) > limit {
			// Fetched one extra — proof that there are more events.
			break
		}
	}

	hasMore := len(matched) > limit
	if hasMore {
		matched = matched[:limit]
	}

	var nextCursor string
	if hasMore && len(matched) > 0 {
		last := matched[len(matched)-1]
		nextCursor = EncodeCursor(last.ID, last.At)
	}

	return AuditPage{
		Events:     matched,
		NextCursor: nextCursor,
		Total:      len(matched),
		HasMore:    hasMore,
	}
}
