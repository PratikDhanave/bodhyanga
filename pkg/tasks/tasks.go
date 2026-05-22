// Package tasks provides a small in-memory work queue for clinical staff tasks
// (refills, prior auths, inbox follow-ups, TCM scheduling) that the Virtual
// Office agents create.
package tasks

import (
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Status is the lifecycle of a NurseTask.
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusCancelled  Status = "cancelled"
)

// NurseTask is one unit of work assigned to clinical staff.
type NurseTask struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"` // tcm_follow_up, refill, prior_auth, inbox, gap_outreach
	PatientID  string    `json:"patient_id"`
	CaseID     string    `json:"case_id,omitempty"`
	Priority   string    `json:"priority"` // low/normal/high/urgent
	DueAt      time.Time `json:"due_at"`
	Status     Status    `json:"status"`
	AssignedTo string    `json:"assigned_to,omitempty"`
	Subject    string    `json:"subject"`
	Detail     string    `json:"detail,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Queue is a thread-safe in-memory store for NurseTasks.
type Queue struct {
	mu    sync.RWMutex
	tasks map[string]NurseTask
}

// NewQueue returns an empty queue.
func NewQueue() *Queue {
	return &Queue{tasks: make(map[string]NurseTask)}
}

// Create assigns an ID + CreatedAt and stores the task.
func (q *Queue) Create(t NurseTask) NurseTask {
	q.mu.Lock()
	defer q.mu.Unlock()
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	if t.Status == "" {
		t.Status = StatusOpen
	}
	q.tasks[t.ID] = t
	return t
}

// UpdateStatus sets a task's status.
func (q *Queue) UpdateStatus(id string, s Status) (NurseTask, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	t, ok := q.tasks[id]
	if !ok {
		return NurseTask{}, false
	}
	t.Status = s
	q.tasks[id] = t
	return t, true
}

// List returns all tasks sorted by DueAt asc.
func (q *Queue) List() []NurseTask {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]NurseTask, 0, len(q.tasks))
	for _, t := range q.tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DueAt.Before(out[j].DueAt) })
	return out
}

// FilterOpen returns open tasks ordered by DueAt asc.
func (q *Queue) FilterOpen() []NurseTask {
	all := q.List()
	out := all[:0]
	for _, t := range all {
		if t.Status == StatusOpen || t.Status == StatusInProgress {
			out = append(out, t)
		}
	}
	return out
}

// CountByKind returns a histogram of task kinds for analytics.
func (q *Queue) CountByKind() map[string]int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := map[string]int{}
	for _, t := range q.tasks {
		out[t.Kind]++
	}
	return out
}
