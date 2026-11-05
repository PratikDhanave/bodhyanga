package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
)

// TestAuditEndpoint_ListAndFilter exercises the /audit handler end-to-end:
// records a small mix of event kinds into an in-memory recorder, mounts
// the handler the same way main.go does, then asserts the unfiltered list
// returns all events and the ?kind= filter narrows correctly. ?limit= is
// also covered.
func TestAuditEndpoint_ListAndFilter(t *testing.T) {
	rec := audit.NewInMemory()
	_ = rec.Record(audit.Event{Kind: audit.KindMessage, AgentID: "intake", CaseID: "c-1", TenantID: "t-1"})
	_ = rec.Record(audit.Event{Kind: audit.KindPolicyDecision, Decision: "deny", CaseID: "c-1", TenantID: "t-1"})
	_ = rec.Record(audit.Event{Kind: audit.KindAgentError, AgentID: "intake", ErrorText: "boom", CaseID: "c-1", TenantID: "t-1"})

	handler := auditListHandler(rec)

	// Unfiltered: 3 events.
	rec1 := httptest.NewRecorder()
	handler(rec1, httptest.NewRequest(http.MethodGet, "/audit", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("status: %d", rec1.Code)
	}
	var all []audit.Event
	if err := json.Unmarshal(rec1.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec1.Body.String())
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(all), all)
	}

	// Filter by kind.
	rec2 := httptest.NewRecorder()
	handler(rec2, httptest.NewRequest(http.MethodGet, "/audit?kind=policy_decision", nil))
	var denies []audit.Event
	_ = json.Unmarshal(rec2.Body.Bytes(), &denies)
	if len(denies) != 1 || denies[0].Decision != "deny" {
		t.Fatalf("expected 1 policy_decision, got %+v", denies)
	}

	// Limit.
	rec3 := httptest.NewRecorder()
	handler(rec3, httptest.NewRequest(http.MethodGet, "/audit?limit=2", nil))
	var limited []audit.Event
	_ = json.Unmarshal(rec3.Body.Bytes(), &limited)
	if len(limited) != 2 {
		t.Fatalf("expected 2 events under limit, got %d", len(limited))
	}

	// Bad limit value falls back to default; no panic.
	rec4 := httptest.NewRecorder()
	handler(rec4, httptest.NewRequest(http.MethodGet, "/audit?limit=not-a-number", nil))
	if rec4.Code != http.StatusOK {
		t.Fatalf("status: %d", rec4.Code)
	}
}

// TestParseLimit_RejectsBadInput covers the limit parser's contract:
// positive integers parse, anything else returns an error (and the
// /audit handler falls back to the default).
func TestParseLimit_RejectsBadInput(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"10", 10, true},
		{"1", 1, true},
		{"0", 0, false},     // zero is not a useful limit
		{"-5", 0, false},
		{"abc", 0, false},
		{"", 0, false},
	} {
		got, err := parseLimit(c.in)
		ok := err == nil
		if ok != c.ok {
			t.Errorf("parseLimit(%q): ok=%v want=%v err=%v", c.in, ok, c.ok, err)
		}
		if c.ok && got != c.want {
			t.Errorf("parseLimit(%q): got %d want %d", c.in, got, c.want)
		}
	}
}

// TestSelectBackends_DefaultsToInMemory asserts that with pg=nil
// selectBackends returns the in-memory recorder + store (the demo path),
// and logs the choice in a recognizable way.
func TestSelectBackends_DefaultsToInMemory(t *testing.T) {
	logger := &captureLogger{}
	rec, store := selectBackends(nil, logger)
	if rec == nil {
		t.Fatal("expected non-nil recorder")
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if !strings.Contains(logger.String(), "in-memory") {
		t.Errorf("expected in-memory backend log, got: %q", logger.String())
	}
	// Recorder is usable.
	if err := rec.Record(audit.Event{Kind: audit.KindMessage, AgentID: "x"}); err != nil {
		t.Errorf("in-memory record error: %v", err)
	}
	if got := rec.List(); len(got) != 1 {
		t.Errorf("expected 1 event, got %d", len(got))
	}
}

// captureLogger is a Printf-compatible logger that joins formatted strings
// for substring assertions.
type captureLogger struct{ lines []string }

func (c *captureLogger) Printf(format string, args ...any) {
	c.lines = append(c.lines, joinFormat(format, args...))
}

func (c *captureLogger) String() string { return strings.Join(c.lines, "\n") }

// joinFormat avoids pulling fmt into the test for one call; the format
// strings are all "[care] ..." so a naive substitution is enough.
func joinFormat(format string, args ...any) string {
	// Very small subset of fmt.Sprintf — enough for the %s/%d patterns we use.
	out := format
	for _, a := range args {
		switch v := a.(type) {
		case string:
			out = strings.Replace(out, "%s", v, 1)
		case int:
			out = strings.Replace(out, "%d", strconv.Itoa(v), 1)
		}
	}
	return out
}

