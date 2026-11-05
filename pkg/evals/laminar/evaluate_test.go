package laminar_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/evals/laminar"
)

// fakeLaminar is a minimal stand-in for the Laminar REST API.
// It handles POST /v1/evals and POST /v1/evals/{id}/datapoints.
type fakeLaminar struct {
	initCalls []map[string]any   // bodies received at /v1/evals
	dpCalls   []map[string]any   // bodies received at /v1/evals/{id}/datapoints
	server    *httptest.Server
}

func newFakeLaminar(t *testing.T) *fakeLaminar {
	t.Helper()
	fl := &fakeLaminar{}
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/evals", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		fl.initCalls = append(fl.initCalls, m)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":        "eval-123",
			"groupId":   "grp-1",
			"name":      "test-eval",
			"projectId": "proj-abc",
			"createdAt": "2026-01-01T00:00:00Z",
		})
	})

	// Catch /v1/evals/{id}/datapoints
	mux.HandleFunc("/v1/evals/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		fl.dpCalls = append(fl.dpCalls, m)
		w.WriteHeader(http.StatusOK)
	})

	fl.server = httptest.NewServer(mux)
	t.Cleanup(fl.server.Close)
	return fl
}

// minimalConfig returns a valid Config pointed at the fake server.
func minimalConfig(fl *fakeLaminar) laminar.Config {
	return laminar.Config{
		Data: []laminar.DataPoint{
			{
				Data:   map[string]any{"country": "Canada", "capital": "Ottawa"},
				Target: map[string]any{"capital": "Ottawa"},
			},
		},
		Executor: func(data map[string]any) (any, error) {
			return data["capital"], nil
		},
		Evaluators: map[string]laminar.EvaluatorFunc{
			"is_correct": func(output any, target map[string]any) float64 {
				if output == target["capital"] {
					return 1
				}
				return 0
			},
		},
		GroupName: "my_first_feature",
		BaseURL:   fl.server.URL,
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestEvaluate_MakesInitCall(t *testing.T) {
	fl := newFakeLaminar(t)
	cfg := minimalConfig(fl)

	if err := laminar.Evaluate(context.Background(), cfg); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(fl.initCalls) != 1 {
		t.Fatalf("want 1 init call, got %d", len(fl.initCalls))
	}
	init := fl.initCalls[0]
	if init["groupName"] != "my_first_feature" {
		t.Errorf("groupName = %v, want my_first_feature", init["groupName"])
	}
}

func TestEvaluate_UploadsDatapoints(t *testing.T) {
	fl := newFakeLaminar(t)
	cfg := minimalConfig(fl)

	if err := laminar.Evaluate(context.Background(), cfg); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(fl.dpCalls) != 1 {
		t.Fatalf("want 1 datapoint call, got %d", len(fl.dpCalls))
	}
	dpBody := fl.dpCalls[0]
	points, ok := dpBody["points"].([]any)
	if !ok || len(points) != 1 {
		t.Fatalf("want 1 point in datapoints body, got %v", dpBody["points"])
	}

	pt := points[0].(map[string]any)

	if pt["executorOutput"] != "Ottawa" {
		t.Errorf("executorOutput = %v, want Ottawa", pt["executorOutput"])
	}
	scores, _ := pt["scores"].(map[string]any)
	if scores["is_correct"] != float64(1) {
		t.Errorf("is_correct = %v, want 1", scores["is_correct"])
	}
	if pt["index"] != float64(0) {
		t.Errorf("index = %v, want 0", pt["index"])
	}
	// id and traceId should be non-empty UUIDs.
	if pt["id"] == "" {
		t.Error("point.id is empty")
	}
	if pt["traceId"] == "" {
		t.Error("point.traceId is empty")
	}
}

func TestEvaluate_MultipleDatapoints(t *testing.T) {
	fl := newFakeLaminar(t)
	cfg := minimalConfig(fl)
	cfg.Data = []laminar.DataPoint{
		{Data: map[string]any{"capital": "Ottawa"}, Target: map[string]any{"capital": "Ottawa"}},
		{Data: map[string]any{"capital": "Paris"}, Target: map[string]any{"capital": "Paris"}},
		{Data: map[string]any{"capital": "Berlin"}, Target: map[string]any{"capital": "Berlin"}},
	}

	if err := laminar.Evaluate(context.Background(), cfg); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	dpBody := fl.dpCalls[0]
	points := dpBody["points"].([]any)
	if len(points) != 3 {
		t.Errorf("want 3 points, got %d", len(points))
	}
	for i, p := range points {
		pt := p.(map[string]any)
		if pt["index"] != float64(i) {
			t.Errorf("points[%d].index = %v, want %d", i, pt["index"], i)
		}
	}
}

func TestEvaluate_BatchesByMaxBatchSize(t *testing.T) {
	fl := newFakeLaminar(t)
	cfg := minimalConfig(fl)
	// 5 points, batch size 2 → 3 upload calls.
	cfg.Data = make([]laminar.DataPoint, 5)
	for i := range cfg.Data {
		cfg.Data[i] = laminar.DataPoint{
			Data:   map[string]any{"capital": "Ottawa"},
			Target: map[string]any{"capital": "Ottawa"},
		}
	}
	cfg.MaxBatchSize = 2

	if err := laminar.Evaluate(context.Background(), cfg); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(fl.dpCalls) != 3 {
		t.Errorf("want 3 datapoint calls (batches), got %d", len(fl.dpCalls))
	}
}

func TestEvaluate_EvaluatorScores(t *testing.T) {
	fl := newFakeLaminar(t)
	cfg := minimalConfig(fl)
	cfg.Evaluators = map[string]laminar.EvaluatorFunc{
		"exact_match": func(output any, target map[string]any) float64 {
			if output == target["capital"] {
				return 1
			}
			return 0
		},
		"always_one": func(any, map[string]any) float64 { return 1.0 },
	}

	if err := laminar.Evaluate(context.Background(), cfg); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	pt := fl.dpCalls[0]["points"].([]any)[0].(map[string]any)
	scores := pt["scores"].(map[string]any)
	if len(scores) != 2 {
		t.Errorf("want 2 scores, got %d: %v", len(scores), scores)
	}
}

func TestEvaluate_UsesAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if strings.HasSuffix(r.URL.Path, "/datapoints") {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "eval-1", "groupId": "g", "name": "n", "projectId": "p", "createdAt": "2026-01-01T00:00:00Z",
		})
	}))
	defer srv.Close()

	cfg := laminar.Config{
		Data:          []laminar.DataPoint{{Data: map[string]any{"x": 1}, Target: map[string]any{"x": 1}}},
		Executor:      func(d map[string]any) (any, error) { return d["x"], nil },
		Evaluators:    map[string]laminar.EvaluatorFunc{"s": func(any, map[string]any) float64 { return 1 }},
		ProjectAPIKey: "test-key-abc",
		BaseURL:       srv.URL,
	}

	if err := laminar.Evaluate(context.Background(), cfg); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if gotAuth != "Bearer test-key-abc" {
		t.Errorf("Authorization = %q, want Bearer test-key-abc", gotAuth)
	}
}

func TestEvaluate_RejectsEmptyData(t *testing.T) {
	cfg := laminar.Config{
		Data:       nil,
		Executor:   func(map[string]any) (any, error) { return nil, nil },
		Evaluators: map[string]laminar.EvaluatorFunc{"s": func(any, map[string]any) float64 { return 0 }},
	}
	if err := laminar.Evaluate(context.Background(), cfg); err == nil {
		t.Error("want error for empty Data, got nil")
	}
}

func TestEvaluate_RejectsNilExecutor(t *testing.T) {
	cfg := laminar.Config{
		Data:       []laminar.DataPoint{{Data: map[string]any{}, Target: map[string]any{}}},
		Executor:   nil,
		Evaluators: map[string]laminar.EvaluatorFunc{"s": func(any, map[string]any) float64 { return 0 }},
	}
	if err := laminar.Evaluate(context.Background(), cfg); err == nil {
		t.Error("want error for nil Executor, got nil")
	}
}
