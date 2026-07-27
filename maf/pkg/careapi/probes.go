package careapi

import "net/http"

// healthz is liveness: the process is up. readyz is readiness: the handler is
// wired. They match cmd/care's probe contract (bodh pkg/runtime/doc.go); the
// pipeline is in-process, so readiness has no external dependency to gate on.
func (a *API) healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (a *API) readyz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
