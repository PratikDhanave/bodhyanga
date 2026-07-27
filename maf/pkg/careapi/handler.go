package careapi

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/PratikDhanave/bodh/pkg/audit"

	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

// API wraps a pipeline.Runner as an HTTP service. The Runner and its audit
// recorder are shared; per-session answer serialization (US3) guards the
// Runner's single mutable Responder field.
type API struct {
	runner  *pipeline.Runner
	reg     Registry
	rec     audit.Recorder
	// execMu serializes ALL pipeline executions (submit's Run and answer's
	// Resume). The Runner wraps one in-process workflow + one checkpoint
	// manager + a single mutable Responder field — none safe for interleaved
	// runs. Serializing keeps cases fully independent (distinct tokens, isolated
	// trails); it just doesn't execute them in parallel. True concurrency is a
	// future scaling concern (a Runner pool), out of scope for this slice.
	execMu  sync.Mutex
	started sync.Map // sessionID → Entry, the sessions this API launched (recovery set)
}

// New builds the HTTP handler over a Runner. The recorder must be the same one
// the Runner writes, so the audit read reflects the pipeline's trail.
func New(runner *pipeline.Runner, reg Registry, rec audit.Recorder) *API {
	return &API{runner: runner, reg: reg, rec: rec}
}

// Handler returns the routed http.Handler (Go 1.22 method+path patterns).
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/cases", a.submit)
	mux.HandleFunc("GET /v1/cases/{token}", a.status)
	mux.HandleFunc("GET /v1/cases/{token}/audit", a.auditTrail)
	mux.HandleFunc("GET /v1/reviews", a.listReviews)
	mux.HandleFunc("POST /v1/reviews/{session}", a.answerReview)
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /readyz", a.readyz)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
