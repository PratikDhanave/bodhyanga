package auditbridge

import (
	"fmt"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/audit"
)

// The six machine-checkable trail invariants from
// specs/001-slice-safety-boundary/contracts/audit-trail.md. Each returns nil
// or a descriptive error; tests (and future monitors) compose them.

// Row is one expected trail entry for ordered-subsequence checks.
type Row struct {
	Kind     audit.Kind
	Decision string // "" = any
	AgentID  string // "" = any
	Type     string // "" = any
}

func (r Row) matches(e audit.Event) bool {
	return e.Kind == r.Kind &&
		(r.Decision == "" || e.Decision == r.Decision) &&
		(r.AgentID == "" || e.AgentID == r.AgentID) &&
		(r.Type == "" || e.Type == r.Type)
}

// CheckOrderedSubsequence verifies the expected rows appear in order —
// duplicates and interleaved extras are tolerated, gaps are not (invariant 5).
func CheckOrderedSubsequence(events []audit.Event, expected []Row) error {
	i := 0
	for _, e := range events {
		if i < len(expected) && expected[i].matches(e) {
			i++
		}
	}
	if i != len(expected) {
		return fmt.Errorf("trail missing expected row %d of %d: %+v", i+1, len(expected), expected[i])
	}
	return nil
}

// CheckHopCompleteness verifies every executed hop (message row with an
// agent) has an allow decision for that agent strictly earlier (invariant 1).
func CheckHopCompleteness(events []audit.Event) error {
	allowed := map[string]int{} // agent → allow count seen so far
	for _, e := range events {
		switch {
		case e.Kind == audit.KindPolicyDecision && e.Decision == "allow":
			allowed[e.AgentID]++
		case e.Kind == audit.KindMessage && e.AgentID != "" && e.Type != "" &&
			e.Type != "checkpoint" && e.Type != "review_requested" && e.Type != "case_finalized":
			if allowed[e.AgentID] == 0 {
				return fmt.Errorf("hop executed without prior allow: agent=%s type=%s", e.AgentID, e.Type)
			}
			allowed[e.AgentID]--
		}
	}
	return nil
}

// CheckDenyTerminal verifies no execution row for an agent follows that
// agent's denial (invariant 2).
func CheckDenyTerminal(events []audit.Event) error {
	denied := map[string]bool{}
	for _, e := range events {
		if e.Kind == audit.KindPolicyDecision && e.Decision == "deny" {
			denied[e.AgentID] = true
			continue
		}
		if e.Kind == audit.KindMessage && denied[e.AgentID] {
			return fmt.Errorf("agent %s executed after denial", e.AgentID)
		}
	}
	return nil
}

// CheckPHIFree scans every string field of every event for forbidden values
// (raw identifiers, clinical text) — invariant 3.
func CheckPHIFree(events []audit.Event, forbidden ...string) error {
	for i, e := range events {
		fields := []string{e.ID, e.AgentID, e.MessageID, e.From, e.To, e.Type,
			e.TenantID, e.PatientID, e.CaseID, e.TraceID, e.Decision, e.Reason,
			e.ReviewerID, e.ErrorText}
		for _, f := range fields {
			for _, bad := range forbidden {
				if bad != "" && strings.Contains(f, bad) {
					return fmt.Errorf("event %d leaks %q in field value %q", i, bad, f)
				}
			}
		}
	}
	return nil
}

// CheckReviewAnchored verifies every review decision names a reviewer or is
// an audited stale rejection (invariant 4).
func CheckReviewAnchored(events []audit.Event) error {
	for i, e := range events {
		if e.Kind == audit.KindReviewDecision && e.ReviewerID == "" && e.Reason != "stale_response" {
			return fmt.Errorf("event %d: review decision without reviewer", i)
		}
	}
	return nil
}
