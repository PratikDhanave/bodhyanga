package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/protocol"

	"github.com/google/uuid"
)

// traceEvent is one wire-format event sent to SPA clients over SSE.
type traceEvent struct {
	TS             string         `json:"ts"`
	From           string         `json:"from"`
	To             string         `json:"to"`
	Type           string         `json:"type"`
	Role           string         `json:"role"`
	ContentPreview string         `json:"content_preview,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// runState tracks one active diagnostic run. Events are buffered so SSE
// clients that connect after the run starts receive the full history; new
// events fan out to all current subscribers.
type runState struct {
	ID        string
	CaseID    string
	Provider  string
	StartedAt time.Time

	mu          sync.Mutex
	events      []traceEvent
	subscribers map[chan traceEvent]struct{}
	closed      bool
}

func newRunState(id, caseID, provider string) *runState {
	return &runState{
		ID:          id,
		CaseID:      caseID,
		Provider:    provider,
		StartedAt:   time.Now().UTC(),
		subscribers: make(map[chan traceEvent]struct{}),
	}
}

func (r *runState) push(ev traceEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	for ch := range r.subscribers {
		select {
		case ch <- ev:
		default: // never block on a slow subscriber
		}
	}
	if ev.Type == "diagnosis" {
		r.markClosedLocked()
	}
}

func (r *runState) snapshot() []traceEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]traceEvent, len(r.events))
	copy(out, r.events)
	return out
}

func (r *runState) subscribe() (<-chan traceEvent, func(), []traceEvent) {
	ch := make(chan traceEvent, 64)
	r.mu.Lock()
	r.subscribers[ch] = struct{}{}
	closed := r.closed
	snap := append([]traceEvent(nil), r.events...)
	r.mu.Unlock()
	cancel := func() {
		r.mu.Lock()
		delete(r.subscribers, ch)
		r.mu.Unlock()
		close(ch)
	}
	if closed {
		// Send one synthetic "closed" event so the SSE client tears down.
		// Use the cancel function to close the channel after the snapshot.
	}
	return ch, cancel, snap
}

func (r *runState) markClosedLocked() { r.closed = true }

// runManager owns all in-flight runs and the bus subscription that funnels
// agent messages into them. Subscribes to the broadcast topic (agentID "")
// so it sees every message; filters by msg.Metadata["case_id"] to route
// each event to the correct run.
type runManager struct {
	mu   sync.RWMutex
	runs map[string]*runState
}

func newRunManager(bus *comm.InMemoryBus) *runManager {
	rm := &runManager{runs: map[string]*runState{}}
	// Broadcast subscriber: every message on the bus.
	bus.Subscribe("", func(_ context.Context, msg protocol.Message) {
		caseID, _ := msg.Metadata["case_id"].(string)
		if caseID == "" {
			return
		}
		rm.mu.RLock()
		var run *runState
		for _, r := range rm.runs {
			if r.CaseID == caseID {
				run = r
				break
			}
		}
		rm.mu.RUnlock()
		if run == nil {
			return
		}
		run.push(traceEvent{
			TS:             time.Now().UTC().Format(time.RFC3339Nano),
			From:           msg.From,
			To:             msg.To,
			Type:           msg.Type,
			Role:           string(msg.Role),
			ContentPreview: truncate(msg.Content, 320),
			Metadata:       sanitizeMeta(msg.Metadata),
		})
	})
	return rm
}

func (m *runManager) register(r *runState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[r.ID] = r
}

func (m *runManager) get(id string) (*runState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runs[id]
	return r, ok
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// sanitizeMeta drops keys that are huge (case_state JSON) so SSE frames stay
// small. The full state is reachable via the bus itself when needed.
func sanitizeMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		if k == "case_state" {
			continue
		}
		out[k] = v
	}
	return out
}

// runRequest is the JSON body for POST /api/runs.
type runRequest struct {
	Fixture       string                  `json:"fixture,omitempty"`
	Diagnostician string                  `json:"diagnostician,omitempty"`
	Presentation  *medical.PatientPresentation `json:"presentation,omitempty"`
	MaxCostUSD    float64                 `json:"max_cost_usd,omitempty"`
	GoldDiagnosis string                  `json:"gold_diagnosis,omitempty"`
}

// startRunHandler returns a handler that publishes the presentation to the
// "intake" agent and registers a new run for SSE consumption.
func startRunHandler(bus *comm.InMemoryBus, rm *runManager, fixturesDir, configuredProvider string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req runRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var pres medical.PatientPresentation
		var caseID, gold string
		var maxCost float64
		// One run id up front so the fixture case id can be made unique per run.
		// The run manager correlates bus messages to a run by case_id, so two
		// runs of the same fixture (case_id "demo-case-001") would otherwise
		// route each other's events to the wrong run state.
		runID := uuid.New().String()

		if req.Fixture != "" {
			f, ok, err := loadFixtureByID(fixturesDir, req.Fixture)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, "fixture not found", http.StatusNotFound)
				return
			}
			pres = medical.PatientPresentation{
				ChiefComplaint:  f.ChiefComplaint,
				HPI:             f.HPI,
				Demographics:    f.Demographics,
				Vitals:          f.Vitals,
				Condition:       f.Condition,
				RPMObservations: f.RPMObservations,
			}
			caseID = f.CaseID + "-" + runID[:8]
			gold = f.GoldDiagnosis
			maxCost = f.MaxCostUSD
		} else {
			if req.Presentation == nil {
				http.Error(w, "either fixture or presentation is required", http.StatusBadRequest)
				return
			}
			pres = *req.Presentation
			caseID = "ui-" + uuid.New().String()[:8]
			gold = req.GoldDiagnosis
			maxCost = req.MaxCostUSD
			if maxCost == 0 {
				maxCost = 500
			}
		}

		provider := configuredProvider
		if req.Diagnostician != "" && req.Diagnostician != provider {
			// Provider is fixed at server startup; record the request value
			// but actually route through the configured agent.
		}

		rs := newRunState(runID, caseID, provider)
		rm.register(rs)

		meta := map[string]any{
			"case_id":        caseID,
			"trace_id":       "ui-" + runID,
			"max_cost_usd":   maxCost,
			"gold_diagnosis": gold,
			"message_steps":  12,
			"run_id":         runID,
		}
		bus.Publish(r.Context(), agent.NewMessage(
			"user", "intake", agent.RoleUser, "presentation",
			medical.EncodePresentation(pres), meta,
		))

		writeJSON(w, map[string]any{
			"run_id":        runID,
			"case_id":       caseID,
			"diagnostician": provider,
			"started_at":    rs.StartedAt.Format(time.RFC3339Nano),
		})
	}
}

// runEventsHandler is the SSE endpoint that streams traceEvents to the SPA.
// On connect it flushes the buffered history, then forwards new events.
func runEventsHandler(rm *runManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/runs/")
		id = strings.TrimSuffix(id, "/events")
		run, ok := rm.get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		ch, cancel, snap := run.subscribe()
		defer cancel()

		// Replay history.
		for _, ev := range snap {
			writeSSE(w, ev)
		}
		flusher.Flush()

		ctx := r.Context()
		// Keep-alive pulse every 15s so proxies don't time out.
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case ev, open := <-ch:
				if !open {
					return
				}
				writeSSE(w, ev)
				flusher.Flush()
			case <-ticker.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, ev traceEvent) {
	b, _ := json.Marshal(ev)
	fmt.Fprintf(w, "data: %s\n\n", string(b))
}
