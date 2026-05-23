package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
)

// TestRunManager_FunnelsMessagesMatchingCaseID asserts the broadcast subscriber
// registered by newRunManager routes a message to the run whose CaseID matches
// the message's metadata["case_id"], and ignores other case_ids.
func TestRunManager_FunnelsMessagesMatchingCaseID(t *testing.T) {
	bus := comm.NewInMemoryBus()
	rm := newRunManager(bus)
	run := newRunState("run-1", "case-A", "rule")
	rm.register(run)

	ctx := context.Background()
	bus.Publish(ctx, agent.NewMessage("user", "intake", agent.RoleUser, "presentation",
		"hello-A", map[string]any{"case_id": "case-A"}))
	bus.Publish(ctx, agent.NewMessage("user", "intake", agent.RoleUser, "presentation",
		"hello-B", map[string]any{"case_id": "case-B"}))

	ok := waitForCondition(t, 2*time.Second, func() bool {
		return len(run.snapshot()) == 1
	})
	if !ok {
		t.Fatalf("timed out waiting for case-A event; snapshot=%+v", run.snapshot())
	}
	got := run.snapshot()
	if got[0].ContentPreview != "hello-A" {
		t.Errorf("expected case-A preview to be 'hello-A', got %q", got[0].ContentPreview)
	}
	if cid, _ := got[0].Metadata["case_id"].(string); cid != "case-A" {
		t.Errorf("expected metadata case_id=case-A, got %v", got[0].Metadata)
	}
}

// TestRunManager_DropsLargeCaseStateFromMetadata verifies sanitizeMeta strips
// the huge case_state blob from the metadata it relays to SSE clients.
func TestRunManager_DropsLargeCaseStateFromMetadata(t *testing.T) {
	bus := comm.NewInMemoryBus()
	rm := newRunManager(bus)
	run := newRunState("run-2", "case-X", "rule")
	rm.register(run)

	bus.Publish(context.Background(), agent.NewMessage(
		"user", "intake", agent.RoleUser, "presentation", "payload",
		map[string]any{"case_id": "case-X", "case_state": `{"big":"blob"}`, "patient_id": "p1"},
	))

	ok := waitForCondition(t, 2*time.Second, func() bool {
		return len(run.snapshot()) == 1
	})
	if !ok {
		t.Fatalf("timed out waiting for event")
	}
	ev := run.snapshot()[0]
	if _, present := ev.Metadata["case_state"]; present {
		t.Errorf("case_state should be sanitized out; metadata=%+v", ev.Metadata)
	}
	if pid, _ := ev.Metadata["patient_id"].(string); pid != "p1" {
		t.Errorf("expected patient_id retained, got %+v", ev.Metadata)
	}
}

// TestRunState_SubscribeReceivesLiveAndReplaysHistory checks that subscribe()
// returns a historical snapshot and that subsequent push() events arrive on
// the channel; cancel() must safely close the subscription.
func TestRunState_SubscribeReceivesLiveAndReplaysHistory(t *testing.T) {
	rs := newRunState("run-3", "case-Y", "rule")
	for i := 0; i < 3; i++ {
		rs.push(traceEvent{Type: "presentation", ContentPreview: "ev"})
	}
	ch, cancel, snap := rs.subscribe()
	if len(snap) != 3 {
		t.Fatalf("expected 3 buffered events, got %d", len(snap))
	}

	rs.push(traceEvent{Type: "questions", ContentPreview: "live"})
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before event")
		}
		if ev.Type != "questions" {
			t.Errorf("expected live event type=questions, got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for live event")
	}

	cancel()
	// Subsequent pushes must not block or panic now that the subscriber is
	// gone. We push and assert it returns quickly via a deadline channel.
	done := make(chan struct{})
	go func() {
		rs.push(traceEvent{Type: "after-cancel"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("push() blocked after subscriber cancelled")
	}
	// And the channel must already be closed (cancel closes it).
	if _, open := <-ch; open {
		t.Errorf("expected channel closed after cancel")
	}
}

// TestRunState_TerminalDiagnosisMarksClosed verifies that pushing a
// type="diagnosis" event flips the closed flag without breaking subscribe().
func TestRunState_TerminalDiagnosisMarksClosed(t *testing.T) {
	rs := newRunState("run-4", "case-Z", "rule")
	rs.push(traceEvent{Type: "diagnosis", ContentPreview: "done"})

	rs.mu.Lock()
	closed := rs.closed
	rs.mu.Unlock()
	if !closed {
		t.Errorf("expected closed=true after diagnosis event")
	}

	ch, cancel, snap := rs.subscribe()
	defer cancel()
	if len(snap) != 1 || snap[0].Type != "diagnosis" {
		t.Errorf("expected diagnosis event in snapshot, got %+v", snap)
	}
	// Channel should still be usable for further pushes.
	rs.push(traceEvent{Type: "trailing"})
	select {
	case ev := <-ch:
		if ev.Type != "trailing" {
			t.Errorf("expected trailing event, got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for trailing event")
	}
}

// TestStartRunHandler_WithFixtureBootsThePipeline writes a fixture, POSTs to
// /api/runs with that case_id, and asserts (a) HTTP 200 with a run_id and
// (b) a "presentation" message landed on the bus tagged with case_id.
func TestStartRunHandler_WithFixtureBootsThePipeline(t *testing.T) {
	s, bus, _ := newTestAPIServer(t)
	writeFixture(t, s.fixturesDir, "alpha", fixtureFile{
		CaseID:         "case-alpha",
		Condition:      "cap",
		ChiefComplaint: "cough",
		HPI:            "3d productive cough",
		MaxCostUSD:     200,
		GoldDiagnosis:  "community acquired pneumonia",
	})

	// Subscribe to the broadcast topic to observe the publish.
	gotMsg := make(chan agent.Message, 4)
	bus.Subscribe("", func(_ context.Context, m agent.Message) {
		// runManager also listens; we just capture in parallel.
		if m.Type == "presentation" {
			select {
			case gotMsg <- m:
			default:
			}
		}
	})

	body := strings.NewReader(`{"fixture":"case-alpha","diagnostician":"rule"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/runs", body)
	rec := httptest.NewRecorder()
	startRunHandler(s.bus, s.runs, s.fixturesDir, s.provider)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if id, _ := resp["run_id"].(string); id == "" {
		t.Errorf("expected non-empty run_id, got %+v", resp)
	}
	if cid, _ := resp["case_id"].(string); cid != "case-alpha" {
		t.Errorf("expected case_id=case-alpha, got %v", resp["case_id"])
	}

	select {
	case m := <-gotMsg:
		if m.Type != "presentation" {
			t.Errorf("expected type=presentation, got %q", m.Type)
		}
		if cid, _ := m.Metadata["case_id"].(string); cid != "case-alpha" {
			t.Errorf("expected message case_id=case-alpha, got %v", m.Metadata["case_id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not observe presentation publish on bus")
	}
}

// TestStartRunHandler_WithPresentationGeneratesCaseID asserts that posting an
// ad-hoc presentation (no fixture) yields a generated "ui-" case_id.
func TestStartRunHandler_WithPresentationGeneratesCaseID(t *testing.T) {
	s, _, _ := newTestAPIServer(t)
	body := strings.NewReader(`{"presentation":{"chief_complaint":"cough","hpi":"2d"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/runs", body)
	rec := httptest.NewRecorder()
	startRunHandler(s.bus, s.runs, s.fixturesDir, s.provider)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if id, _ := resp["run_id"].(string); id == "" {
		t.Errorf("missing run_id: %+v", resp)
	}
	cid, _ := resp["case_id"].(string)
	if !strings.HasPrefix(cid, "ui-") {
		t.Errorf("expected case_id with ui- prefix, got %q", cid)
	}
}

// TestStartRunHandler_RejectsNeitherFixtureNorPresentation expects HTTP 400
// when the body is empty.
func TestStartRunHandler_RejectsNeitherFixtureNorPresentation(t *testing.T) {
	s, _, _ := newTestAPIServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	startRunHandler(s.bus, s.runs, s.fixturesDir, s.provider)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestRunEventsHandler_StreamsSSE asserts the SSE stream emits two events
// pushed onto the runState after the subscription is established.
func TestRunEventsHandler_StreamsSSE(t *testing.T) {
	bus := comm.NewInMemoryBus()
	rm := newRunManager(bus)
	rs := newRunState("sse-run", "case-sse", "rule")
	rm.register(rs)

	srv := httptest.NewServer(http.HandlerFunc(runEventsHandler(rm)))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/runs/sse-run/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	// Wait until the SSE handler has registered its subscriber on the run.
	if ok := waitForCondition(t, 2*time.Second, func() bool {
		rs.mu.Lock()
		defer rs.mu.Unlock()
		return len(rs.subscribers) > 0
	}); !ok {
		t.Fatal("SSE handler never subscribed")
	}

	// Push two live events; the handler should write a `data: {...}` frame
	// per event.
	rs.push(traceEvent{Type: "questions", ContentPreview: "q1"})
	rs.push(traceEvent{Type: "diagnosis", ContentPreview: "d1"})

	// Read frames until we've seen both expected types or the deadline hits.
	frames := make(chan traceEvent, 8)
	readErr := make(chan error, 1)
	go func() {
		defer close(frames)
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					readErr <- err
				}
				return
			}
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			var ev traceEvent
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				continue
			}
			frames <- ev
		}
	}()

	gotQuestions, gotDiagnosis := false, false
	deadline := time.After(2 * time.Second)
	for !(gotQuestions && gotDiagnosis) {
		select {
		case ev, open := <-frames:
			if !open {
				t.Fatalf("stream ended before both frames; questions=%v diagnosis=%v", gotQuestions, gotDiagnosis)
			}
			switch ev.Type {
			case "questions":
				gotQuestions = true
			case "diagnosis":
				gotDiagnosis = true
			}
		case err := <-readErr:
			t.Fatalf("stream read error: %v", err)
		case <-deadline:
			t.Fatalf("timeout; questions=%v diagnosis=%v", gotQuestions, gotDiagnosis)
		}
	}
}
