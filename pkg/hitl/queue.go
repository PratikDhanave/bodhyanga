// Package hitl implements the human-in-the-loop review queue. Clinical
// actions that must be reviewed before the platform acts on them are stored
// here as medical.ReviewRequest; a reviewer (clinician or nurse) decides via
// HTTP or programmatic API, and the gate agent resumes the original action.
package hitl

import (
	"sort"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/google/uuid"
)

// Queue is a thread-safe in-memory store of ReviewRequests.
type Queue struct {
	mu       sync.RWMutex
	requests map[string]medical.ReviewRequest
}

// NewQueue returns an empty queue.
func NewQueue() *Queue {
	return &Queue{requests: make(map[string]medical.ReviewRequest)}
}

// Submit stores a new request with status=pending and returns it (with id and
// SubmittedAt populated).
func (q *Queue) Submit(r medical.ReviewRequest) medical.ReviewRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.SubmittedAt.IsZero() {
		r.SubmittedAt = time.Now().UTC()
	}
	r.Status = medical.ReviewPending
	q.requests[r.ID] = r
	return r
}

// Decide records a reviewer's choice. decision must be one of
// "approve", "approve_with_modifications", "reject". Returns the updated
// request and true on success; (zero, false) if the id is unknown, already
// decided, or the decision string is invalid.
func (q *Queue) Decide(id, reviewerID, decision, reason, modification string) (medical.ReviewRequest, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	r, ok := q.requests[id]
	if !ok || r.Status != medical.ReviewPending {
		return r, false
	}
	switch decision {
	case "approve":
		r.Status = medical.ReviewApproved
	case "approve_with_modifications":
		r.Status = medical.ReviewModified
	case "reject":
		r.Status = medical.ReviewRejected
	default:
		return r, false
	}
	r.ReviewerID = reviewerID
	r.Reason = reason
	r.Modification = modification
	r.DecidedAt = time.Now().UTC()
	q.requests[id] = r
	return r, true
}

// Get returns a request by id.
func (q *Queue) Get(id string) (medical.ReviewRequest, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	r, ok := q.requests[id]
	return r, ok
}

// List returns every request sorted by SubmittedAt asc.
func (q *Queue) List() []medical.ReviewRequest {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]medical.ReviewRequest, 0, len(q.requests))
	for _, r := range q.requests {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SubmittedAt.Before(out[j].SubmittedAt) })
	return out
}

// Pending returns only requests with status=pending, sorted by SubmittedAt asc.
func (q *Queue) Pending() []medical.ReviewRequest {
	all := q.List()
	out := all[:0]
	for _, r := range all {
		if r.Status == medical.ReviewPending {
			out = append(out, r)
		}
	}
	return out
}

// CountByKind returns a histogram of all requests, grouped by Kind.
func (q *Queue) CountByKind() map[string]int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := map[string]int{}
	for _, r := range q.requests {
		out[r.Kind]++
	}
	return out
}

// CountByStatus returns a histogram grouped by Status.
func (q *Queue) CountByStatus() map[medical.ReviewStatus]int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := map[medical.ReviewStatus]int{}
	for _, r := range q.requests {
		out[r.Status]++
	}
	return out
}

// Expire scans pending requests and marks any past their SLA as expired.
// Returns how many were transitioned.
func (q *Queue) Expire(now time.Time) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for id, r := range q.requests {
		if r.Status != medical.ReviewPending {
			continue
		}
		if !r.SLAAt.IsZero() && now.After(r.SLAAt) {
			r.Status = medical.ReviewExpired
			r.DecidedAt = now
			q.requests[id] = r
			n++
		}
	}
	return n
}
