package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---- NewServer (0%) ----

func TestNewServer(t *testing.T) {
	cfg := Config{
		Card: AgentCard{Name: "test-agent"},
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			return "ok", nil
		},
		Addr: ":8099",
	}
	srv := NewServer(cfg)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.Addr != ":8099" {
		t.Errorf("addr = %q, want :8099", srv.Addr)
	}
}

// ---- submitTask error paths ----

func TestSubmitTask_InvalidBody(t *testing.T) {
	h := Handler(Config{
		Card:    AgentCard{Name: "test"},
		Handler: func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader("not json"))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestSubmitTask_NilInput(t *testing.T) {
	h := Handler(Config{
		Card:    AgentCard{Name: "test"},
		Handler: func(_ context.Context, input map[string]any) (string, error) { return "ok", nil },
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{}`))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rr.Code)
	}
}

// ---- routeTask paths ----

func TestRouteTask_EmptyPath(t *testing.T) {
	h := Handler(Config{
		Card:    AgentCard{Name: "test"},
		Handler: func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks/", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestRouteTask_GetNonExistent(t *testing.T) {
	h := Handler(Config{
		Card:    AgentCard{Name: "test"},
		Handler: func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks/nonexistent", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

// ---- processTask with handler error ----

func TestProcessTask_HandlerError(t *testing.T) {
	handler := Handler(Config{
		Card: AgentCard{Name: "test"},
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			return "", errors.New("handler failed")
		},
	})

	// Submit a task.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"input":{"key":"val"}}`))
	handler.ServeHTTP(rr, req)

	var task Task
	json.NewDecoder(rr.Body).Decode(&task)

	// Wait for background processing.
	time.Sleep(200 * time.Millisecond)

	// Get the task — should be in failed state.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/tasks/"+task.ID, nil)
	handler.ServeHTTP(rr2, req2)

	var updated Task
	json.NewDecoder(rr2.Body).Decode(&updated)
	if updated.State != TaskStateFailed {
		t.Errorf("state = %q, want failed", updated.State)
	}
	if updated.Error == "" {
		t.Error("expected non-empty error")
	}
}

// ---- processTask success path ----

func TestProcessTask_Success(t *testing.T) {
	handler := Handler(Config{
		Card: AgentCard{Name: "test"},
		Handler: func(_ context.Context, input map[string]any) (string, error) {
			return "result text", nil
		},
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"input":{"key":"val"}}`))
	handler.ServeHTTP(rr, req)

	var task Task
	json.NewDecoder(rr.Body).Decode(&task)

	time.Sleep(200 * time.Millisecond)

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/tasks/"+task.ID, nil)
	handler.ServeHTTP(rr2, req2)

	var updated Task
	json.NewDecoder(rr2.Body).Decode(&updated)
	if updated.State != TaskStateCompleted {
		t.Errorf("state = %q, want completed", updated.State)
	}
	if updated.Output == nil || updated.Output.Text != "result text" {
		t.Errorf("unexpected output: %+v", updated.Output)
	}
}

// ---- streamTask non-existent ----

func TestStreamTask_NotFound(t *testing.T) {
	handler := Handler(Config{
		Card:    AgentCard{Name: "test"},
		Handler: func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks/nonexistent/stream", nil)
	ctx, cancel := context.WithTimeout(req.Context(), 500*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	handler.ServeHTTP(rr, req)
	// Should contain an error event.
	body := rr.Body.String()
	if !strings.Contains(body, "error") {
		t.Errorf("expected error event, got %q", body)
	}
}

// ---- streamTask with completion ----

func TestStreamTask_Completion(t *testing.T) {
	handler := Handler(Config{
		Card: AgentCard{Name: "test"},
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			time.Sleep(50 * time.Millisecond)
			return "done", nil
		},
	})

	// Submit task.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"input":{}}`))
	handler.ServeHTTP(rr, req)

	var task Task
	json.NewDecoder(rr.Body).Decode(&task)

	// Stream.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/tasks/"+task.ID+"/stream", nil)
	ctx, cancel := context.WithTimeout(req2.Context(), 2*time.Second)
	defer cancel()
	req2 = req2.WithContext(ctx)

	handler.ServeHTTP(rr2, req2)

	body := rr2.Body.String()
	if !strings.Contains(body, "task") {
		t.Errorf("expected task event in stream, got %q", body)
	}
}

// ---- streamTask client disconnect ----

func TestStreamTask_ClientDisconnect(t *testing.T) {
	handler := Handler(Config{
		Card: AgentCard{Name: "test"},
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			// Slow handler to keep task in working state.
			time.Sleep(2 * time.Second)
			return "done", nil
		},
	})

	// Submit a task.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"input":{}}`))
	handler.ServeHTTP(rr, req)

	var task Task
	json.NewDecoder(rr.Body).Decode(&task)

	// Stream with a very short timeout to simulate client disconnect.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/tasks/"+task.ID+"/stream", nil)
	ctx, cancel := context.WithTimeout(req2.Context(), 200*time.Millisecond)
	defer cancel()
	req2 = req2.WithContext(ctx)

	handler.ServeHTTP(rr2, req2)
	// Should return without error when context is cancelled.
}
