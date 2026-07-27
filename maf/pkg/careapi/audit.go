package careapi

import "net/http"

// auditEvent is the PHI-free projection of one trail row served over HTTP.
type auditEvent struct {
	Kind      string `json:"kind"`
	AgentID   string `json:"agent_id,omitempty"`
	Type      string `json:"type,omitempty"`
	Decision  string `json:"decision,omitempty"`
	Reason    string `json:"reason,omitempty"`
	CaseToken string `json:"case_token,omitempty"`
}

// auditTrail returns the case-filtered, ordered, PHI-free trail. The bodh audit
// recorder is already PHI-free (case ids are hashed tokens); we filter to the
// requested token and project the safe fields. 404 distinguishes an unknown
// case from one with an empty trail (FR-010).
func (a *API) auditTrail(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	_, known := a.reg.Get(token)

	var events []auditEvent
	for _, e := range a.rec.List() {
		if e.CaseID != token {
			continue
		}
		known = true
		events = append(events, auditEvent{
			Kind:      string(e.Kind),
			AgentID:   e.AgentID,
			Type:      e.Type,
			Decision:  e.Decision,
			Reason:    e.Reason,
			CaseToken: e.CaseID,
		})
	}
	if !known {
		writeErr(w, http.StatusNotFound, "unknown case")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"case_token": token, "events": events})
}
