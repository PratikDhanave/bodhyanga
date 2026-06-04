package laminar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// Evaluate: validation errors
// ---------------------------------------------------------------------------

func TestEvaluate_EmptyData(t *testing.T) {
	err := Evaluate(context.Background(), Config{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestEvaluate_NilExecutor(t *testing.T) {
	err := Evaluate(context.Background(), Config{
		Data: []DataPoint{{Data: map[string]any{"x": 1}}},
	})
	if err == nil {
		t.Error("expected error for nil executor")
	}
}

func TestEvaluate_EmptyEvaluators(t *testing.T) {
	err := Evaluate(context.Background(), Config{
		Data:     []DataPoint{{Data: map[string]any{"x": 1}}},
		Executor: func(d map[string]any) (any, error) { return nil, nil },
	})
	if err == nil {
		t.Error("expected error for empty evaluators")
	}
}

// ---------------------------------------------------------------------------
// Evaluate: HTTP error on init
// ---------------------------------------------------------------------------

func TestEvaluate_HTTPErrorOnInit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	err := Evaluate(context.Background(), Config{
		Data:     []DataPoint{{Data: map[string]any{"x": 1}}},
		Executor: func(d map[string]any) (any, error) { return "out", nil },
		Evaluators: map[string]EvaluatorFunc{
			"score": func(output any, target map[string]any) float64 { return 1.0 },
		},
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	if err == nil {
		t.Error("expected error for HTTP 500")
	}
}

// ---------------------------------------------------------------------------
// Evaluate: executor error
// ---------------------------------------------------------------------------

func TestEvaluate_ExecutorError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// Init endpoint
			json.NewEncoder(w).Encode(initEvalResponse{ID: "test-id"})
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	err := Evaluate(context.Background(), Config{
		Data: []DataPoint{{Data: map[string]any{"x": 1}}},
		Executor: func(d map[string]any) (any, error) {
			return nil, fmt.Errorf("exec failed")
		},
		Evaluators: map[string]EvaluatorFunc{
			"score": func(output any, target map[string]any) float64 { return 1.0 },
		},
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	if err == nil {
		t.Error("expected error from executor")
	}
}

// ---------------------------------------------------------------------------
// Evaluate: full success with batching
// ---------------------------------------------------------------------------

func TestEvaluate_SuccessWithBatching(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(initEvalResponse{ID: "test-id"})
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Create 5 data points with MaxBatchSize=2 to test batching.
	data := make([]DataPoint, 5)
	for i := range data {
		data[i] = DataPoint{
			Data:   map[string]any{"x": i},
			Target: map[string]any{"y": i},
		}
	}

	err := Evaluate(context.Background(), Config{
		Data: data,
		Executor: func(d map[string]any) (any, error) {
			return d["x"], nil
		},
		Evaluators: map[string]EvaluatorFunc{
			"exact": func(output any, target map[string]any) float64 {
				if output == target["y"] {
					return 1.0
				}
				return 0.0
			},
		},
		BaseURL:      srv.URL,
		MaxBatchSize: 2,
		HTTPClient:   srv.Client(),
		GroupName:    "test-group",
		Name:         "test-name",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect: 1 init + 3 batch uploads (2+2+1).
	if callCount != 4 {
		t.Errorf("expected 4 HTTP calls (1 init + 3 batches), got %d", callCount)
	}
}

// ---------------------------------------------------------------------------
// Evaluate: upload batch HTTP error
// ---------------------------------------------------------------------------

func TestEvaluate_UploadBatchError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(initEvalResponse{ID: "test-id"})
			return
		}
		w.WriteHeader(413)
		w.Write([]byte("payload too large"))
	}))
	defer srv.Close()

	err := Evaluate(context.Background(), Config{
		Data: []DataPoint{{Data: map[string]any{"x": 1}}},
		Executor: func(d map[string]any) (any, error) {
			return "ok", nil
		},
		Evaluators: map[string]EvaluatorFunc{
			"score": func(output any, target map[string]any) float64 { return 1.0 },
		},
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	if err == nil {
		t.Error("expected error for upload failure")
	}
}

// ---------------------------------------------------------------------------
// Evaluate: exactly MaxBatchSize items
// ---------------------------------------------------------------------------

func TestEvaluate_ExactlyMaxBatchSize(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(initEvalResponse{ID: "test-id"})
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	data := make([]DataPoint, 3)
	for i := range data {
		data[i] = DataPoint{Data: map[string]any{"x": i}}
	}

	err := Evaluate(context.Background(), Config{
		Data: data,
		Executor: func(d map[string]any) (any, error) {
			return "ok", nil
		},
		Evaluators: map[string]EvaluatorFunc{
			"score": func(output any, target map[string]any) float64 { return 1.0 },
		},
		BaseURL:      srv.URL,
		MaxBatchSize: 3,
		HTTPClient:   srv.Client(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 init + 1 batch (exactly MaxBatchSize).
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}
