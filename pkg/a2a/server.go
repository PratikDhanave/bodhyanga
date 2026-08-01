// Package a2a implements a minimal Google A2A (Agent-to-Agent) protocol server.
//
// Routes:
//
//	GET  /.well-known/agent.json  — agent card
//	POST /tasks                    — submit a new task
//	GET  /tasks/{id}               — get task status
//	GET  /tasks/{id}/stream        — SSE stream of task updates
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config holds server configuration.
type Config struct {
	// Card is the AgentCard served at /.well-known/agent.json.
	Card AgentCard
	// Handler processes task inputs and returns text output.
	// It receives (ctx, input map[string]any) and returns (output string, err error).
	Handler func(ctx context.Context, input map[string]any) (string, error)
	// Addr is the listen address, e.g. ":8099".
	Addr string
}

// NewServer returns an *http.Server configured for A2A.
func NewServer(cfg Config) *http.Server {
	return &http.Server{
		Addr:    cfg.Addr,
		Handler: Handler(cfg),
	}
}

// Handler returns an http.Handler for use with existing HTTP servers.
func Handler(cfg Config) http.Handler {
	s := &a2aHandler{
		cfg: cfg,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agent.json", s.serveCard)
	mux.HandleFunc("POST /tasks", s.submitTask)
	mux.HandleFunc("GET /tasks/", s.routeTask)
	return mux
}

// a2aHandler holds server state.
type a2aHandler struct {
	cfg   Config
	tasks sync.Map // map[string]*Task

	// mu guards the mutable fields of every *Task stored in tasks. sync.Map
	// only synchronizes map operations, not the structs its pointers reach —
	// processTask mutates a task from a background goroutine while handlers
	// read it, so all field access is serialized through mu.
	mu sync.Mutex
}

// snapshot returns a value copy of a task taken under mu, so callers can
// read or marshal it without racing processTask's background mutations.
func (s *a2aHandler) snapshot(id string) (Task, bool) {
	v, ok := s.tasks.Load(id)
	if !ok {
		return Task{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return *v.(*Task), true
}

// newTaskID generates a unique task ID.
func newTaskID() string {
	return fmt.Sprintf("task-%d", time.Now().UnixNano())
}

// now returns the current time formatted as RFC3339.
func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// serveCard serves the AgentCard at /.well-known/agent.json.
func (s *a2aHandler) serveCard(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.Card)
}

// submitTask handles POST /tasks — creates a task and begins background processing.
func (s *a2aHandler) submitTask(w http.ResponseWriter, r *http.Request) {
	var req SendTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}

	t := &Task{
		ID:      newTaskID(),
		State:   TaskStateSubmitted,
		Input:   req.Input,
		Created: now(),
		Updated: now(),
	}
	s.tasks.Store(t.ID, t)

	// Process asynchronously.
	go s.processTask(t.ID)

	// Marshal a snapshot, not the live pointer: processTask may already be
	// mutating t on another goroutine.
	snap, _ := s.snapshot(t.ID)
	writeJSON(w, http.StatusAccepted, snap)
}

// routeTask dispatches GET /tasks/{id} and GET /tasks/{id}/stream.
func (s *a2aHandler) routeTask(w http.ResponseWriter, r *http.Request) {
	// Strip leading "/tasks/"
	path := strings.TrimPrefix(r.URL.Path, "/tasks/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	if strings.HasSuffix(path, "/stream") {
		id := strings.TrimSuffix(path, "/stream")
		s.streamTask(w, r, id)
		return
	}

	s.getTask(w, r, path)
}

// getTask handles GET /tasks/{id}.
func (s *a2aHandler) getTask(w http.ResponseWriter, r *http.Request, id string) {
	snap, ok := s.snapshot(id)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found: "+id)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// streamTask handles GET /tasks/{id}/stream — SSE stream of state changes.
func (s *a2aHandler) streamTask(w http.ResponseWriter, r *http.Request, id string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Poll until task reaches a terminal state or client disconnects.
	ctx := r.Context()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastState TaskState

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			task, ok := s.snapshot(id)
			if !ok {
				// Send not-found event and close.
				writeSSEEvent(w, "error", map[string]string{"error": "task not found: " + id})
				flusher.Flush()
				return
			}

			// Send an event whenever the state changes.
			if task.State != lastState {
				lastState = task.State
				writeSSEEvent(w, "task", task)
				flusher.Flush()
			}

			// Stop streaming when the task has reached a terminal state.
			if task.State == TaskStateCompleted || task.State == TaskStateFailed {
				return
			}
		}
	}
}

// processTask runs the handler in a background goroutine and updates task state.
func (s *a2aHandler) processTask(id string) {
	v, ok := s.tasks.Load(id)
	if !ok {
		return
	}
	task := v.(*Task)

	// Transition to working. All field mutations happen under s.mu so
	// handlers reading via snapshot() never observe a torn write. Input is
	// set once at creation and never mutated, so reading it lock-free is safe.
	s.mu.Lock()
	task.State = TaskStateWorking
	task.Updated = now()
	s.mu.Unlock()

	output, err := s.cfg.Handler(context.Background(), task.Input)

	// Transition to terminal state.
	s.mu.Lock()
	task.Updated = now()
	if err != nil {
		task.State = TaskStateFailed
		task.Error = err.Error()
	} else {
		task.State = TaskStateCompleted
		task.Output = &TaskOutput{Text: output}
	}
	s.mu.Unlock()
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeSSEEvent writes a single Server-Sent Event.
func writeSSEEvent(w http.ResponseWriter, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}
