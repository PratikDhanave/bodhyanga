package audit

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"
)

// TestDecodeCursor_Empty tests empty cursor.
func TestDecodeCursor_Empty(t *testing.T) {
	_, _, err := DecodeCursor("")
	if err == nil {
		t.Fatal("expected error for empty cursor")
	}
}

// TestDecodeCursor_InvalidBase64 tests invalid base64 cursor.
func TestDecodeCursor_InvalidBase64(t *testing.T) {
	_, _, err := DecodeCursor("!!!invalid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

// TestDecodeCursor_MissingDelimiter tests cursor missing pipe delimiter.
func TestDecodeCursor_MissingDelimiter(t *testing.T) {
	encoded := base64.URLEncoding.EncodeToString([]byte("nopipedelimiter"))
	_, _, err := DecodeCursor(encoded)
	if err == nil {
		t.Fatal("expected error for missing delimiter")
	}
}

// TestDecodeCursor_EmptyID tests cursor with empty ID.
func TestDecodeCursor_EmptyID(t *testing.T) {
	encoded := base64.URLEncoding.EncodeToString([]byte("|2024-01-01T00:00:00Z"))
	_, _, err := DecodeCursor(encoded)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

// TestDecodeCursor_BadTimestamp tests cursor with an unparseable timestamp.
func TestDecodeCursor_BadTimestamp(t *testing.T) {
	encoded := base64.URLEncoding.EncodeToString([]byte("some-id|not-a-timestamp"))
	_, _, err := DecodeCursor(encoded)
	if err == nil {
		t.Fatal("expected error for bad timestamp")
	}
}

// TestPageFromRecorder_NoEvents tests pagination with no events.
func TestPageFromRecorder_NoEvents(t *testing.T) {
	rec := NewInMemory()
	page := PageFromRecorder(rec, AuditQuery{TenantID: "t1"})
	if page.Total != 0 {
		t.Fatalf("expected 0 events, got %d", page.Total)
	}
	if page.HasMore {
		t.Fatal("expected no more events")
	}
	if page.NextCursor != "" {
		t.Fatal("expected empty NextCursor")
	}
}

// TestPageFromRecorder_AllFilters tests all filter combinations.
func TestPageFromRecorder_AllFilters(t *testing.T) {
	rec := NewInMemory()
	now := time.Now()

	// Event matching all filters.
	rec.Record(Event{
		Kind:     KindMessage,
		AgentID:  "agent-1",
		CaseID:   "case-1",
		TenantID: "t1",
		At:       now,
	})
	// Event with wrong tenant.
	rec.Record(Event{
		Kind:     KindMessage,
		AgentID:  "agent-1",
		CaseID:   "case-1",
		TenantID: "t2",
	})
	// Event with wrong kind.
	rec.Record(Event{
		Kind:     KindPolicyDecision,
		AgentID:  "agent-1",
		CaseID:   "case-1",
		TenantID: "t1",
	})
	// Event with wrong case_id.
	rec.Record(Event{
		Kind:     KindMessage,
		AgentID:  "agent-1",
		CaseID:   "case-2",
		TenantID: "t1",
	})
	// Event with wrong agent_id.
	rec.Record(Event{
		Kind:     KindMessage,
		AgentID:  "agent-2",
		CaseID:   "case-1",
		TenantID: "t1",
	})

	page := PageFromRecorder(rec, AuditQuery{
		TenantID: "t1",
		CaseID:   "case-1",
		AgentID:  "agent-1",
		Kind:     string(KindMessage),
	})
	if page.Total != 1 {
		t.Fatalf("expected 1 matching event, got %d", page.Total)
	}
}

// TestPageFromRecorder_CursorPagination tests cursor-based pagination.
func TestPageFromRecorder_CursorPagination(t *testing.T) {
	rec := NewInMemory()
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		rec.Record(Event{
			ID:       fmt.Sprintf("evt-%d", i),
			Kind:     KindMessage,
			TenantID: "t1",
			At:       base.Add(time.Duration(i) * time.Second),
		})
	}

	// First page, limit 2.
	page1 := PageFromRecorder(rec, AuditQuery{TenantID: "t1", Limit: 2})
	if page1.Total != 2 {
		t.Fatalf("page1: expected 2 events, got %d", page1.Total)
	}
	if !page1.HasMore {
		t.Fatal("page1: expected HasMore")
	}
	if page1.NextCursor == "" {
		t.Fatal("page1: expected NextCursor")
	}

	// Second page using cursor.
	page2 := PageFromRecorder(rec, AuditQuery{TenantID: "t1", Limit: 2, After: page1.NextCursor})
	if page2.Total != 2 {
		t.Fatalf("page2: expected 2 events, got %d", page2.Total)
	}

	// Third page.
	page3 := PageFromRecorder(rec, AuditQuery{TenantID: "t1", Limit: 2, After: page2.NextCursor})
	if page3.Total != 1 {
		t.Fatalf("page3: expected 1 event, got %d", page3.Total)
	}
	if page3.HasMore {
		t.Fatal("page3: expected no more")
	}
}

// TestPageFromRecorder_MalformedCursor tests behavior with malformed cursor.
func TestPageFromRecorder_MalformedCursor(t *testing.T) {
	rec := NewInMemory()
	rec.Record(Event{Kind: KindMessage, TenantID: "t1"})

	// Malformed cursor is silently ignored — treated as start from beginning.
	page := PageFromRecorder(rec, AuditQuery{TenantID: "t1", After: "not-valid-base64!!!"})
	if page.Total != 1 {
		t.Fatalf("expected 1 event despite bad cursor, got %d", page.Total)
	}
}

// TestResolvedLimit tests limit resolution logic.
func TestResolvedLimit_Defaults(t *testing.T) {
	q := AuditQuery{}
	if n := q.ResolvedLimit(); n != DefaultAuditPageLimit {
		t.Fatalf("expected %d, got %d", DefaultAuditPageLimit, n)
	}

	q.Limit = -5
	if n := q.ResolvedLimit(); n != DefaultAuditPageLimit {
		t.Fatalf("negative limit: expected %d, got %d", DefaultAuditPageLimit, n)
	}

	q.Limit = MaxAuditPageLimit + 100
	if n := q.ResolvedLimit(); n != MaxAuditPageLimit {
		t.Fatalf("over-limit: expected %d, got %d", MaxAuditPageLimit, n)
	}

	q.Limit = 10
	if n := q.ResolvedLimit(); n != 10 {
		t.Fatalf("normal: expected 10, got %d", n)
	}
}
