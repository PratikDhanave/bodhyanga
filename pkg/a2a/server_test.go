package a2a_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/a2a"
)

// echoHandler returns "Echo: " + input["message"].
func echoHandler(_ context.Context, input map[string]any) (string, error) {
	msg, _ := input["message"].(string)
	return "Echo: " + msg, nil
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := a2a.Config{
		Card: a2a.AgentCard{
			Name:        "test-agent",
			Description: "Test agent",
			Version:     "0.1.0",
			Capabilities: a2a.Capabilities{
				Streaming: true,
			},
			Skills: []a2a.Skill{
				{ID: "echo", Name: "Echo", Description: "Echoes input"},
			},
		},
		Handler: echoHandler,
	}
	return httptest.NewServer(a2a.Handler(cfg))
}

// TestAgentCard verifies the agent card endpoint.
func TestAgentCard(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("GET /.well-known/agent.json: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}
	if card.Name != "test-agent" {
		t.Errorf("expected name=test-agent, got %q", card.Name)
	}
	if !card.Capabilities.Streaming {
		t.Error("expected streaming=true")
	}
	if len(card.Skills) != 1 || card.Skills[0].ID != "echo" {
		t.Errorf("unexpected skills: %+v", card.Skills)
	}
}

// TestSubmitTask verifies task submission and polling for completion.
func TestSubmitTask(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body, _ := json.Marshal(a2a.SendTaskRequest{
		Input: map[string]any{"message": "hello"},
	})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /tasks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	var task a2a.Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if task.ID == "" {
		t.Fatal("task ID is empty")
	}

	// Poll GET /tasks/{id} until completed (or timeout).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pollResp, err := http.Get(srv.URL + "/tasks/" + task.ID)
		if err != nil {
			t.Fatalf("GET /tasks/%s: %v", task.ID, err)
		}

		var polled a2a.Task
		if err := json.NewDecoder(pollResp.Body).Decode(&polled); err != nil {
			pollResp.Body.Close()
			t.Fatalf("decode polled task: %v", err)
		}
		pollResp.Body.Close()

		if polled.State == a2a.TaskStateCompleted {
			if polled.Output == nil {
				t.Fatal("completed task has nil output")
			}
			expected := "Echo: hello"
			if polled.Output.Text != expected {
				t.Errorf("expected output %q, got %q", expected, polled.Output.Text)
			}
			return
		}
		if polled.State == a2a.TaskStateFailed {
			t.Fatalf("task failed: %s", polled.Error)
		}

		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for task to complete")
}

// TestGetTaskNotFound verifies that unknown task IDs return 404.
func TestGetTaskNotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks/nonexistent-task")
	if err != nil {
		t.Fatalf("GET /tasks/nonexistent-task: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// TestSSEStream verifies that the SSE stream endpoint delivers task-update events.
func TestSSEStream(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Submit a task.
	body, _ := json.Marshal(a2a.SendTaskRequest{
		Input: map[string]any{"message": "stream test"},
	})
	postResp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /tasks: %v", err)
	}
	var task a2a.Task
	if err := json.NewDecoder(postResp.Body).Decode(&task); err != nil {
		t.Fatalf("decode submitted task: %v", err)
	}
	postResp.Body.Close()

	// Open the SSE stream.
	streamURL := fmt.Sprintf("%s/tasks/%s/stream", srv.URL, task.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	streamResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tasks/%s/stream: %v", task.ID, err)
	}
	defer streamResp.Body.Close()

	ct := streamResp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected Content-Type text/event-stream, got %q", ct)
	}

	// Read SSE lines and look for a completed event.
	buf := make([]byte, 4096)
	found := false
	for !found {
		n, readErr := streamResp.Body.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			// Each SSE payload line starts with "data: ".
			for _, line := range strings.Split(chunk, "\n") {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				payload := strings.TrimPrefix(line, "data: ")
				var ev a2a.Task
				if jsonErr := json.Unmarshal([]byte(payload), &ev); jsonErr != nil {
					continue
				}
				if ev.State == a2a.TaskStateCompleted {
					found = true
					if ev.Output == nil || ev.Output.Text != "Echo: stream test" {
						t.Errorf("unexpected output: %+v", ev.Output)
					}
					break
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	if !found {
		t.Error("did not receive a completed task event on the SSE stream")
	}
}
