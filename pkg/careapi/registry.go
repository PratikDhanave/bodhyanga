package careapi

import "sync"

// Status is the lifecycle state of a submitted case, as seen over HTTP.
type Status string

const (
	StatusRunning        Status = "running"
	StatusAwaitingReview Status = "awaiting_review"
	StatusCompleted      Status = "completed"
	StatusRejected       Status = "rejected"
	StatusDenied         Status = "denied"
	StatusFailed         Status = "failed"
)

// Entry is the PHI-free projection of a case the API tracks and serves. Every
// field is safe to return over HTTP: CaseToken/SessionID are opaque
// fingerprints, Diagnosis/PlanSteps are clinical decision-support output (not
// patient identifiers), and no raw case id, name, or narrative is ever stored
// here (FR-002/FR-004). The submit and resume paths are the only writers.
type Entry struct {
	CaseToken  string   `json:"case_token"`
	SessionID  string   `json:"session_id"`
	Status     Status   `json:"status"`
	Diagnosis  string   `json:"diagnosis,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Forced     bool     `json:"forced,omitempty"`
	ReviewerID string   `json:"reviewer_id,omitempty"`
	PlanSteps  []string `json:"plan_steps,omitempty"`
	Note       string   `json:"note,omitempty"`
	CostUSD    float64  `json:"cost_usd,omitempty"` // total test cost (specs/006 Pareto axis)
}

// Registry stores case Entries keyed by the opaque case token, with a
// secondary index by session id (the handle the review/resume path uses).
type Registry interface {
	Put(e Entry)
	Get(caseToken string) (Entry, bool)
	BySession(sessionID string) (Entry, bool)
	Pending() []Entry
}

// memRegistry is the default in-process, concurrency-safe Registry. A durable
// store is a future swap-in; the Runner's checkpoint store is the source of
// truth for paused cases (see recover.go), so this may be rebuilt on restart.
type memRegistry struct {
	mu       sync.RWMutex
	byToken  map[string]Entry
	tokenFor map[string]string // sessionID → caseToken
}

// NewRegistry returns an empty in-memory Registry.
func NewRegistry() Registry {
	return &memRegistry{
		byToken:  make(map[string]Entry),
		tokenFor: make(map[string]string),
	}
}

func (r *memRegistry) Put(e Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byToken[e.CaseToken] = e
	if e.SessionID != "" {
		r.tokenFor[e.SessionID] = e.CaseToken
	}
}

func (r *memRegistry) Get(caseToken string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byToken[caseToken]
	return e, ok
}

func (r *memRegistry) BySession(sessionID string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	token, ok := r.tokenFor[sessionID]
	if !ok {
		return Entry{}, false
	}
	e, ok := r.byToken[token]
	return e, ok
}

func (r *memRegistry) Pending() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Entry
	for _, e := range r.byToken {
		if e.Status == StatusAwaitingReview {
			out = append(out, e)
		}
	}
	return out
}
