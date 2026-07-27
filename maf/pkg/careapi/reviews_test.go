package careapi

import (
	"strings"
	"testing"
)

// submitPaused submits the canonical case and returns its handle; with Review
// on, a produced diagnosis parks at awaiting_review.
func submitPaused(t *testing.T, ta *testAPI) handleResponse {
	t.Helper()
	var h handleResponse
	ta.doJSON(t, "POST", "/v1/cases", chestPain, &h)
	if h.Status != StatusAwaitingReview {
		t.Fatalf("case did not park for review: %+v", h)
	}
	return h
}

type reviewsBody struct {
	Reviews []reviewSummary `json:"reviews"`
}

func TestReviews_ListShowsPausedCase(t *testing.T) {
	ta := newTestAPI(t, nil)
	h := submitPaused(t, ta)

	var body reviewsBody
	code := ta.doJSON(t, "GET", "/v1/reviews", nil, &body)
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(body.Reviews) != 1 || body.Reviews[0].SessionID != h.SessionID {
		t.Fatalf("reviews = %+v, want the one paused case", body.Reviews)
	}
	// PHI-free projection.
	blob, _ := marshal(body)
	for _, phi := range []string{"PT-SECRET-42", "chest pain"} {
		if strings.Contains(blob, phi) {
			t.Errorf("review list leaked %q", phi)
		}
	}
}

func TestReviews_ApproveResumesToCompletion(t *testing.T) {
	ta := newTestAPI(t, nil)
	h := submitPaused(t, ta)

	var e Entry
	code := ta.doJSON(t, "POST", "/v1/reviews/"+h.SessionID,
		answerRequest{Approved: true, ReviewerID: "dr-house"}, &e)
	if code != 200 {
		t.Fatalf("approve status = %d, want 200", code)
	}
	if e.Status != StatusCompleted {
		t.Errorf("status = %q, want completed", e.Status)
	}
	if e.ReviewerID != "dr-house" || len(e.PlanSteps) == 0 {
		t.Errorf("approved entry incomplete: %+v", e)
	}
	// The review has left the pending list.
	var body reviewsBody
	ta.doJSON(t, "GET", "/v1/reviews", nil, &body)
	if len(body.Reviews) != 0 {
		t.Errorf("approved case still pending: %+v", body.Reviews)
	}
}

func TestReviews_RejectLeavesNoPlan(t *testing.T) {
	ta := newTestAPI(t, nil)
	h := submitPaused(t, ta)
	var e Entry
	ta.doJSON(t, "POST", "/v1/reviews/"+h.SessionID,
		answerRequest{Approved: false, ReviewerID: "dr-wilson"}, &e)
	if e.Status != StatusRejected {
		t.Errorf("status = %q, want rejected", e.Status)
	}
	if len(e.PlanSteps) != 0 {
		t.Errorf("rejected case must have no plan, got %v", e.PlanSteps)
	}
}

func TestReviews_ValidationErrors(t *testing.T) {
	ta := newTestAPI(t, nil)
	h := submitPaused(t, ta)

	// empty reviewer → 400, nothing applied.
	if code, _ := ta.do(t, "POST", "/v1/reviews/"+h.SessionID, answerRequest{Approved: true}); code != 400 {
		t.Errorf("empty reviewer status = %d, want 400", code)
	}
	// unknown session → 404.
	if code, _ := ta.do(t, "POST", "/v1/reviews/ghost", answerRequest{Approved: true, ReviewerID: "x"}); code != 404 {
		t.Errorf("unknown session status = %d, want 404", code)
	}
	// answer once, then again → 409 (no longer awaiting).
	var e Entry
	ta.doJSON(t, "POST", "/v1/reviews/"+h.SessionID, answerRequest{Approved: true, ReviewerID: "dr-a"}, &e)
	if code, _ := ta.do(t, "POST", "/v1/reviews/"+h.SessionID, answerRequest{Approved: true, ReviewerID: "dr-b"}); code != 409 {
		t.Errorf("second answer status = %d, want 409", code)
	}
}
