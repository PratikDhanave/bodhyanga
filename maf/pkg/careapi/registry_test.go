package careapi

import (
	"fmt"
	"sync"
	"testing"
)

func TestRegistry_RoundTripAndSessionIndex(t *testing.T) {
	r := NewRegistry()
	e := Entry{CaseToken: "abc123", SessionID: "sess-1", Status: StatusRunning, Diagnosis: "acs"}
	r.Put(e)

	got, ok := r.Get("abc123")
	if !ok || got.Diagnosis != "acs" {
		t.Fatalf("Get = %+v ok=%v", got, ok)
	}
	bySess, ok := r.BySession("sess-1")
	if !ok || bySess.CaseToken != "abc123" {
		t.Fatalf("BySession = %+v ok=%v", bySess, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Error("Get on unknown token should be false")
	}
	if _, ok := r.BySession("missing"); ok {
		t.Error("BySession on unknown session should be false")
	}
}

func TestRegistry_PendingReturnsOnlyAwaitingReview(t *testing.T) {
	r := NewRegistry()
	r.Put(Entry{CaseToken: "t1", SessionID: "s1", Status: StatusAwaitingReview})
	r.Put(Entry{CaseToken: "t2", SessionID: "s2", Status: StatusCompleted})
	r.Put(Entry{CaseToken: "t3", SessionID: "s3", Status: StatusAwaitingReview})

	pending := r.Pending()
	if len(pending) != 2 {
		t.Fatalf("Pending len = %d, want 2", len(pending))
	}
	for _, e := range pending {
		if e.Status != StatusAwaitingReview {
			t.Errorf("Pending returned non-awaiting entry: %+v", e)
		}
	}
}

// Put overwrites: a case that advances from running → completed keeps one row.
func TestRegistry_PutOverwrites(t *testing.T) {
	r := NewRegistry()
	r.Put(Entry{CaseToken: "t", SessionID: "s", Status: StatusAwaitingReview})
	r.Put(Entry{CaseToken: "t", SessionID: "s", Status: StatusCompleted, Diagnosis: "cap"})
	got, _ := r.Get("t")
	if got.Status != StatusCompleted || got.Diagnosis != "cap" {
		t.Errorf("overwrite failed: %+v", got)
	}
	if len(r.Pending()) != 0 {
		t.Error("completed case should not be pending")
	}
}

// -race guard: concurrent writers and readers must not data-race.
func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tok := fmt.Sprintf("t%d", n)
			r.Put(Entry{CaseToken: tok, SessionID: fmt.Sprintf("s%d", n), Status: StatusRunning})
			_, _ = r.Get(tok)
			_ = r.Pending()
		}(i)
	}
	wg.Wait()
	if _, ok := r.Get("t7"); !ok {
		t.Error("expected t7 present after concurrent writes")
	}
}
