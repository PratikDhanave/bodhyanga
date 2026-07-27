package careapi

import (
	"encoding/json"
	"net/http"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

// handleResponse is the PHI-free handle returned on submit.
type handleResponse struct {
	SessionID string `json:"session_id"`
	CaseToken string `json:"case_token"`
	Status    Status `json:"status"`
}

// submit decodes a PatientCase, drives Runner.Run, records the PHI-free outcome
// in the registry, and returns a handle. A governance denial is a normal 201
// with status "denied" — not a 500 (FR-011): the boundary did its job.
func (a *API) submit(w http.ResponseWriter, r *http.Request) {
	var pc agents.PatientCase
	if err := json.NewDecoder(r.Body).Decode(&pc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid case json")
		return
	}
	a.execMu.Lock()
	out, err := a.runner.Run(r.Context(), pc, "")
	a.execMu.Unlock()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "pipeline error")
		return
	}
	entry := a.outcomeToEntry(a.runner.CaseToken(pc), out)
	a.reg.Put(entry)
	if entry.Status == StatusAwaitingReview {
		a.started.Store(entry.SessionID, entry)
	}
	writeJSON(w, http.StatusCreated, handleResponse{
		SessionID: entry.SessionID,
		CaseToken: entry.CaseToken,
		Status:    entry.Status,
	})
}

// outcomeToEntry projects a terminal or paused Outcome into a PHI-free Entry.
// Precedence matches the Runner's own terminal states.
func (a *API) outcomeToEntry(caseToken string, out *pipeline.Outcome) Entry {
	e := Entry{CaseToken: caseToken, SessionID: out.SessionID}
	switch {
	case out.Denied:
		e.Status = StatusDenied
	case out.Paused:
		e.Status = StatusAwaitingReview
		if req := pendingReview(out); req != nil {
			e.Diagnosis = req.Diagnosis
			e.Confidence = req.Confidence
			e.Forced = req.Forced
			e.CostUSD = req.CostUSD
		}
	case out.Rejected != nil:
		e.Status = StatusRejected
		e.Diagnosis = out.Rejected.Diagnosis
		e.ReviewerID = out.Rejected.ReviewerID
		e.Note = out.Rejected.Note
	case out.Plan != nil:
		e.Status = StatusCompleted
		e.Diagnosis = out.Plan.Diagnosis
		e.Forced = out.Plan.Forced
		e.PlanSteps = out.Plan.Steps
		e.CostUSD = out.Plan.CostUSD
	default:
		e.Status = StatusFailed
	}
	return e
}
