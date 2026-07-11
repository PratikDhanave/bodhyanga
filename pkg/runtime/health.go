package runtime

// File health.go: liveness + readiness probe handlers for a per-agent binary,
// mirroring the /healthz + /readyz contract cmd/care already exposes
// (cmd/care/health.go) so the same K8s manifests and compose healthchecks work
// unchanged against a decoupled agent.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ReadyCheck is one named readiness dependency an agent binary registers with
// ReadinessHandler — e.g. "bus" (NATS connection live) or "postgres" (store
// reachable). It returns nil when the dependency is healthy and an error
// otherwise; the error string is surfaced in the 503 body for debuggability and
// so must never contain PHI.
type ReadyCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

// LivenessHandler serves GET /healthz. It always returns 200 while the process
// is running and performs no I/O — its only job is to let the kubelet tell a
// live process from a hung one (a deadlocked handler eventually times out,
// tripping the liveness probe). Identical semantics to cmd/care's /healthz.
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// ReadinessHandler serves GET /readyz. It runs every registered ReadyCheck and
// returns 200 only when all pass; if any fails it returns 503 naming the first
// failed dependency, which takes the Pod out of the Service endpoint slice
// while it reconnects instead of letting it black-hole traffic. With no checks
// it degrades to liveness (200), matching cmd/care's nil-Postgres path.
//
// Each check is bounded by a 3s timeout derived from the request context so a
// wedged dependency can't hang the probe past the kubelet's own deadline.
func ReadinessHandler(checks ...ReadyCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, c := range checks {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			err := c.Check(ctx)
			cancel()
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"status": "unavailable",
					"reason": c.Name + " check failed",
					"time":   time.Now().UTC().Format(time.RFC3339),
				})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "ready",
			"time":   time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// writeJSON writes a minimal JSON body with the given status. The response
// never carries PHI — only a status string and an RFC3339 timestamp.
func writeJSON(w http.ResponseWriter, status int, body map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
