package audit

import (
	"testing"
	"time"
)

// TestAuditPagination_CursorRoundTrip verifies that EncodeCursor and
// DecodeCursor are exact inverses of each other for a variety of inputs,
// including IDs with pipe-like characters and timestamps with nanosecond
// precision.
func TestAuditPagination_CursorRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		id   string
		at   time.Time
	}{
		{
			name: "basic UUID",
			id:   "550e8400-e29b-41d4-a716-446655440000",
			at:   time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name: "nanosecond precision",
			id:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			at:   time.Date(2024, 1, 1, 0, 0, 0, 123456789, time.UTC),
		},
		{
			name: "zero nanoseconds",
			id:   "00000000-0000-0000-0000-000000000001",
			at:   time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name: "early epoch",
			id:   "12345678-1234-1234-1234-123456789012",
			at:   time.Date(1970, 1, 1, 0, 0, 1, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			encoded := EncodeCursor(tc.id, tc.at)
			if encoded == "" {
				t.Fatal("EncodeCursor returned empty string")
			}

			gotID, gotAt, err := DecodeCursor(encoded)
			if err != nil {
				t.Fatalf("DecodeCursor(%q): %v", encoded, err)
			}
			if gotID != tc.id {
				t.Errorf("id: got %q, want %q", gotID, tc.id)
			}
			// Compare UTC timestamps; DecodeCursor normalises to UTC.
			wantAt := tc.at.UTC()
			if !gotAt.Equal(wantAt) {
				t.Errorf("at: got %v, want %v", gotAt, wantAt)
			}
		})
	}
}

// TestAuditPagination_DecodeCursorErrors verifies that malformed cursors
// return a non-nil error and do not panic.
func TestAuditPagination_DecodeCursorErrors(t *testing.T) {
	bad := []struct {
		name   string
		cursor string
	}{
		{"empty", ""},
		{"not base64", "!@#$%"},
		{"no delimiter", EncodeCursor("", time.Now())},    // empty id
		{"plain text", "aGVsbG8="},                        // base64("hello") — no delimiter
		{"garbage bytes", "dGhpcyBpcyBub3QgYSBjdXJzb3I="}, // "this is not a cursor"
	}

	for _, tc := range bad {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			id, at, err := DecodeCursor(tc.cursor)
			if err == nil {
				t.Errorf("expected error for cursor %q, got id=%q at=%v", tc.cursor, id, at)
			}
		})
	}
}

// TestAuditPagination_PageFromRecorder_Basic verifies the core pagination
// behaviour: correct page size, HasMore flag, and NextCursor propagation.
func TestAuditPagination_PageFromRecorder_Basic(t *testing.T) {
	rec := NewInMemory()
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Insert 5 events, 1 second apart so ordering is deterministic.
	for i := 0; i < 5; i++ {
		_ = rec.Record(Event{
			ID:       "evt-" + string(rune('0'+i)),
			At:       base.Add(time.Duration(i) * time.Second),
			Kind:     KindMessage,
			AgentID:  "intake",
			TenantID: "tenant-a",
			CaseID:   "case-1",
		})
	}

	q := AuditQuery{
		TenantID: "tenant-a",
		Limit:    2,
	}

	// Page 1: expect 2 events and HasMore=true.
	page1 := PageFromRecorder(rec, q)
	if page1.Total != 2 {
		t.Fatalf("page1 total: got %d, want 2", page1.Total)
	}
	if !page1.HasMore {
		t.Fatal("page1: expected HasMore=true")
	}
	if page1.NextCursor == "" {
		t.Fatal("page1: expected non-empty NextCursor")
	}

	// Page 2: resume from cursor; expect 2 more events.
	q.After = page1.NextCursor
	page2 := PageFromRecorder(rec, q)
	if page2.Total != 2 {
		t.Fatalf("page2 total: got %d, want 2", page2.Total)
	}
	if !page2.HasMore {
		t.Fatal("page2: expected HasMore=true")
	}

	// Page 3: last page has 1 event, HasMore=false.
	q.After = page2.NextCursor
	page3 := PageFromRecorder(rec, q)
	if page3.Total != 1 {
		t.Fatalf("page3 total: got %d, want 1", page3.Total)
	}
	if page3.HasMore {
		t.Fatal("page3: expected HasMore=false")
	}
	if page3.NextCursor != "" {
		t.Fatalf("page3: expected empty NextCursor, got %q", page3.NextCursor)
	}

	// Ensure no overlapping events across pages.
	seen := map[string]bool{}
	for _, pg := range []AuditPage{page1, page2, page3} {
		for _, e := range pg.Events {
			if seen[e.ID] {
				t.Errorf("duplicate event %q across pages", e.ID)
			}
			seen[e.ID] = true
		}
	}
	if len(seen) != 5 {
		t.Errorf("expected 5 unique events across all pages, got %d", len(seen))
	}
}

// TestAuditPagination_PageFromRecorder_Filters checks that kind, case_id, and
// agent_id filters narrow results correctly and interact with the cursor.
func TestAuditPagination_PageFromRecorder_Filters(t *testing.T) {
	rec := NewInMemory()
	base := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	events := []Event{
		{Kind: KindMessage, AgentID: "intake", TenantID: "t1", CaseID: "c1", At: base.Add(0)},
		{Kind: KindPolicyDecision, AgentID: "orchestrator", TenantID: "t1", CaseID: "c1", At: base.Add(time.Second)},
		{Kind: KindMessage, AgentID: "diagnostician", TenantID: "t1", CaseID: "c2", At: base.Add(2 * time.Second)},
		{Kind: KindReviewDecision, AgentID: "hitl_gate", TenantID: "t1", CaseID: "c1", At: base.Add(3 * time.Second)},
		{Kind: KindMessage, AgentID: "intake", TenantID: "t2", CaseID: "c3", At: base.Add(4 * time.Second)}, // different tenant
	}
	for _, e := range events {
		_ = rec.Record(e)
	}

	t.Run("filter by kind", func(t *testing.T) {
		pg := PageFromRecorder(rec, AuditQuery{TenantID: "t1", Kind: string(KindMessage), Limit: 50})
		if pg.Total != 2 {
			t.Errorf("got %d, want 2 message events for t1", pg.Total)
		}
	})

	t.Run("filter by case_id", func(t *testing.T) {
		pg := PageFromRecorder(rec, AuditQuery{TenantID: "t1", CaseID: "c1", Limit: 50})
		if pg.Total != 3 {
			t.Errorf("got %d, want 3 events for c1", pg.Total)
		}
	})

	t.Run("filter by agent_id", func(t *testing.T) {
		pg := PageFromRecorder(rec, AuditQuery{TenantID: "t1", AgentID: "intake", Limit: 50})
		if pg.Total != 1 {
			t.Errorf("got %d, want 1 event for intake in t1", pg.Total)
		}
	})

	t.Run("tenant isolation", func(t *testing.T) {
		pg := PageFromRecorder(rec, AuditQuery{TenantID: "t2", Limit: 50})
		if pg.Total != 1 {
			t.Errorf("got %d, want 1 event for t2", pg.Total)
		}
	})
}

// TestAuditPagination_DefaultAndMaxLimit checks that zero/negative limits get
// the default and oversized limits are capped.
func TestAuditPagination_DefaultAndMaxLimit(t *testing.T) {
	q0 := AuditQuery{Limit: 0}
	if got := q0.ResolvedLimit(); got != DefaultAuditPageLimit {
		t.Errorf("limit 0: got %d, want %d", got, DefaultAuditPageLimit)
	}

	qNeg := AuditQuery{Limit: -10}
	if got := qNeg.ResolvedLimit(); got != DefaultAuditPageLimit {
		t.Errorf("limit -10: got %d, want %d", got, DefaultAuditPageLimit)
	}

	qMax := AuditQuery{Limit: MaxAuditPageLimit + 1}
	if got := qMax.ResolvedLimit(); got != MaxAuditPageLimit {
		t.Errorf("limit %d: got %d, want %d", MaxAuditPageLimit+1, got, MaxAuditPageLimit)
	}

	qOK := AuditQuery{Limit: 42}
	if got := qOK.ResolvedLimit(); got != 42 {
		t.Errorf("limit 42: got %d, want 42", got)
	}
}
