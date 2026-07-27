package careapi

import "net/http"

// status resolves a case token to its PHI-free Entry from the registry. No
// Runner call — reads are served from the state submit/resume wrote (FR-003).
func (a *API) status(w http.ResponseWriter, r *http.Request) {
	e, ok := a.reg.Get(r.PathValue("token"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown case")
		return
	}
	writeJSON(w, http.StatusOK, e)
}
