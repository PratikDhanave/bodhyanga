package careapi

import (
	"encoding/json"
	"net/http"

	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

// reviewSummary is the PHI-free projection of a parked case (FR-005).
type reviewSummary struct {
	SessionID  string  `json:"session_id"`
	CaseToken  string  `json:"case_token"`
	Diagnosis  string  `json:"diagnosis"`
	Confidence float64 `json:"confidence"`
	Forced     bool    `json:"forced"`
}

// listReviews returns every case awaiting physician review.
func (a *API) listReviews(w http.ResponseWriter, _ *http.Request) {
	pending := a.reg.Pending()
	out := make([]reviewSummary, 0, len(pending))
	for _, e := range pending {
		out = append(out, reviewSummary{
			SessionID:  e.SessionID,
			CaseToken:  e.CaseToken,
			Diagnosis:  e.Diagnosis,
			Confidence: e.Confidence,
			Forced:     e.Forced,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": out})
}

type answerRequest struct {
	Approved   bool   `json:"approved"`
	ReviewerID string `json:"reviewer_id"`
	Note       string `json:"note"`
}

// answerReview applies a physician's verdict and resumes the paused run,
// durable across a process restart when the Runner has a checkpoint store.
// Validation: empty reviewer → 400 (an anonymous verdict never lands); unknown
// session → 404; session not awaiting review → 409.
func (a *API) answerReview(w http.ResponseWriter, r *http.Request) {
	session := r.PathValue("session")
	var body answerRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid decision json")
		return
	}
	if body.ReviewerID == "" {
		writeErr(w, http.StatusBadRequest, "reviewer_id required")
		return
	}

	a.execMu.Lock()
	defer a.execMu.Unlock()

	// Re-check state under the lock: a concurrent answer that already resolved
	// this session leaves it non-awaiting → 409.
	e, ok := a.reg.BySession(session)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown session")
		return
	}
	if e.Status != StatusAwaitingReview {
		writeErr(w, http.StatusConflict, "session is not awaiting review")
		return
	}

	decision := agents.ReviewDecision{Approved: body.Approved, ReviewerID: body.ReviewerID, Note: body.Note}
	a.runner.Responder = func(req *workflow.ExternalRequest) (*workflow.ExternalResponse, error) {
		return req.CreateResponse(decision)
	}
	out, err := a.runner.Resume(r.Context(), session)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "resume failed")
		return
	}

	entry := a.outcomeToEntry(e.CaseToken, out)
	entry.SessionID = session
	entry.ReviewerID = body.ReviewerID
	a.reg.Put(entry)
	a.started.Delete(session)
	writeJSON(w, http.StatusOK, entry)
}
