// Package laminar provides a Go equivalent of the Python lmnr.evaluate() call.
//
// It runs an executor and a set of evaluators over a dataset locally, then
// ships the results to the Laminar backend in two HTTP calls:
//
//  1. POST /v1/evals          — create the evaluation run, get its ID
//  2. POST /v1/evals/{id}/datapoints — upload scores for every data point
//
// Usage (mirrors the Python signature exactly):
//
//	err := laminar.Evaluate(ctx, laminar.Config{
//	    Data: []laminar.DataPoint{
//	        {
//	            Data:   map[string]any{"country": "Canada", "capital": "Ottawa"},
//	            Target: map[string]any{"capital": "Ottawa"},
//	        },
//	    },
//	    Executor: func(data map[string]any) (any, error) {
//	        return data["capital"], nil
//	    },
//	    Evaluators: map[string]laminar.EvaluatorFunc{
//	        "is_correct": func(output any, target map[string]any) float64 {
//	            if output == target["capital"] { return 1 }
//	            return 0
//	        },
//	    },
//	    GroupName: "my_first_feature",
//	})
//
// Environment variables (all optional when set in Config fields):
//
//	LMNR_PROJECT_API_KEY — project API key
//	LMNR_BASE_URL        — base URL (default: https://api.lmnr.ai;
//	                       self-hosted: http://localhost:5667)
package laminar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

// DataPoint is one row in the evaluation dataset. Matches the Python dict
// with "data" and "target" keys passed to evaluate().
type DataPoint struct {
	// Data is the input passed to the Executor.
	Data map[string]any
	// Target is the ground-truth value passed to each Evaluator alongside
	// the Executor's output.
	Target map[string]any
}

// ExecutorFunc is the function under evaluation. It receives one DataPoint's
// Data map and must return the output to score (any JSON-serialisable value).
// Equivalent to the executor= lambda in Python.
type ExecutorFunc func(data map[string]any) (any, error)

// EvaluatorFunc scores one output against a target. Returns a numeric score —
// typically 0.0 / 1.0 for binary evaluators, or a continuous value for graded
// ones. Equivalent to each function in the evaluators= dict in Python.
type EvaluatorFunc func(output any, target map[string]any) float64

// Config is the input to Evaluate. All fields except Data, Executor, and
// Evaluators have sensible defaults from environment variables.
type Config struct {
	// Data is the evaluation dataset. Required; must be non-empty.
	Data []DataPoint

	// Executor is called once per DataPoint to produce the output being
	// evaluated. Required.
	Executor ExecutorFunc

	// Evaluators maps evaluator names to scoring functions. Required;
	// at least one evaluator must be provided.
	Evaluators map[string]EvaluatorFunc

	// GroupName groups this run with other runs of the same feature or
	// experiment. Equivalent to group_id= in Python (maps to groupName JSON).
	GroupName string

	// Name is an optional human-readable name for this evaluation run.
	Name string

	// ProjectAPIKey authenticates with Laminar. Defaults to LMNR_PROJECT_API_KEY.
	ProjectAPIKey string

	// BaseURL is the Laminar server root URL.
	// Defaults to LMNR_BASE_URL → https://api.lmnr.ai.
	// Self-hosted example: "http://localhost:5667"
	BaseURL string

	// MaxBatchSize is the maximum number of datapoints per upload request.
	// Laminar rejects payloads > ~20 MB; reduce this if you hit 413 errors.
	// Default: 100.
	MaxBatchSize int

	// HTTPClient allows injecting a custom *http.Client (e.g. for tests).
	// Defaults to a client with a 60-second timeout.
	HTTPClient *http.Client
}

// ── wire types ────────────────────────────────────────────────────────────────

type initEvalRequest struct {
	Name      *string `json:"name"`
	GroupName *string `json:"groupName"`
}

type initEvalResponse struct {
	ID        string `json:"id"`
	GroupID   string `json:"groupId"`
	Name      string `json:"name"`
	ProjectID string `json:"projectId"`
	CreatedAt string `json:"createdAt"`
}

type datapointPayload struct {
	ID             string             `json:"id"`
	Data           map[string]any     `json:"data"`
	Target         map[string]any     `json:"target"`
	ExecutorOutput any                `json:"executorOutput"`
	Scores         map[string]float64 `json:"scores"`
	TraceID        string             `json:"traceId"`
	ExecutorSpanID string             `json:"executorSpanId"`
	Index          int                `json:"index"`
}

type datapointsRequest struct {
	Points    []datapointPayload `json:"points"`
	GroupName *string            `json:"groupName"`
}

// ── Evaluate ─────────────────────────────────────────────────────────────────

// Evaluate runs executor on every DataPoint, scores each output with every
// Evaluator, and ships the results to Laminar's evaluation API.
//
// It is the Go equivalent of the Python lmnr.evaluate() call.
//
// The function:
//  1. Creates an evaluation run  (POST /v1/evals)
//  2. Runs Executor + Evaluators locally for every DataPoint
//  3. Uploads results in batches (POST /v1/evals/{id}/datapoints)
func Evaluate(ctx context.Context, cfg Config) error {
	if len(cfg.Data) == 0 {
		return fmt.Errorf("laminar.Evaluate: Data must not be empty")
	}
	if cfg.Executor == nil {
		return fmt.Errorf("laminar.Evaluate: Executor must not be nil")
	}
	if len(cfg.Evaluators) == 0 {
		return fmt.Errorf("laminar.Evaluate: Evaluators must not be empty")
	}

	// Resolve env-var defaults.
	apiKey := cfg.ProjectAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("LMNR_PROJECT_API_KEY")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("LMNR_BASE_URL")
	}
	if baseURL == "" {
		baseURL = "https://api.lmnr.ai"
	}
	maxBatch := cfg.MaxBatchSize
	if maxBatch <= 0 {
		maxBatch = 100
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	do := func(req *http.Request) (*http.Response, error) {
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		return client.Do(req)
	}

	// ── Step 1: create evaluation run ────────────────────────────────────
	initBody := initEvalRequest{}
	if cfg.Name != "" {
		initBody.Name = &cfg.Name
	}
	if cfg.GroupName != "" {
		initBody.GroupName = &cfg.GroupName
	}

	initResp, err := postJSON[initEvalResponse](ctx, client, do, baseURL+"/v1/evals", initBody)
	if err != nil {
		return fmt.Errorf("create eval run: %w", err)
	}
	evalID := initResp.ID

	// ── Step 2: run executor + evaluators locally ─────────────────────────
	points := make([]datapointPayload, 0, len(cfg.Data))
	for i, dp := range cfg.Data {
		output, execErr := cfg.Executor(dp.Data)
		if execErr != nil {
			return fmt.Errorf("executor[%d]: %w", i, execErr)
		}
		scores := make(map[string]float64, len(cfg.Evaluators))
		for name, evalFn := range cfg.Evaluators {
			scores[name] = evalFn(output, dp.Target)
		}
		points = append(points, datapointPayload{
			ID:             uuid.New().String(),
			Data:           dp.Data,
			Target:         dp.Target,
			ExecutorOutput: output,
			Scores:         scores,
			TraceID:        uuid.New().String(),
			ExecutorSpanID: uuid.New().String(),
			Index:          i,
		})
	}

	// ── Step 3: upload in batches ─────────────────────────────────────────
	url := fmt.Sprintf("%s/v1/evals/%s/datapoints", baseURL, evalID)
	var gn *string
	if cfg.GroupName != "" {
		gn = &cfg.GroupName
	}

	for start := 0; start < len(points); start += maxBatch {
		end := start + maxBatch
		if end > len(points) {
			end = len(points)
		}
		batch := datapointsRequest{Points: points[start:end], GroupName: gn}
		if err := postJSONVoid(ctx, client, do, url, batch); err != nil {
			return fmt.Errorf("upload datapoints [%d:%d]: %w", start, end, err)
		}
	}
	return nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

// postJSON POST-encodes body as JSON, sends it, and decodes the response into T.
func postJSON[T any](ctx context.Context, _ *http.Client, do func(*http.Request) (*http.Response, error), url string, body any) (T, error) {
	var zero T
	raw, err := json.Marshal(body)
	if err != nil {
		return zero, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return zero, err
	}
	resp, err := do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return zero, fmt.Errorf("HTTP %s: %s", resp.Status, string(b))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return zero, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}

// postJSONVoid POST-encodes body as JSON and checks for a non-error status.
func postJSONVoid(ctx context.Context, _ *http.Client, do func(*http.Request) (*http.Response, error), url string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	resp, err := do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %s: %s", resp.Status, string(b))
	}
	return nil
}
