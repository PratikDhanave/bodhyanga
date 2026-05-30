// Command laminar-eval is the Go equivalent of the Python lmnr.evaluate() call:
//
//	from lmnr import evaluate
//
//	evaluate(
//	    data=[
//	        {
//	            "data":   {"country": "Canada", "capital": "Ottawa"},
//	            "target": {"capital": "Ottawa"},
//	        }
//	    ],
//	    executor=lambda data: data["capital"],
//	    evaluators={
//	        "is_correct": lambda output, target: int(output == target["capital"])
//	    },
//	    group_id="my_first_feature",
//	    project_api_key="<YOUR_PROJECT_API_KEY>",
//	)
//
// # Run
//
//	go run ./cmd/laminar-eval
//
// # Environment variables
//
//	LMNR_PROJECT_API_KEY — project API key (required)
//	LMNR_BASE_URL        — Laminar server URL (default: https://api.lmnr.ai)
//	                       self-hosted: http://localhost:8000
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/PratikDhanave/bodh/pkg/evals/laminar"
)

func main() {
	groupName := flag.String("group", envOr("LMNR_GROUP_NAME", "my_first_feature"),
		"Laminar group name (equivalent to group_id= in Python)")
	flag.Parse()

	apiKey := os.Getenv("LMNR_PROJECT_API_KEY")
	if apiKey == "" {
		log.Fatal("LMNR_PROJECT_API_KEY must be set (source .env or export it)")
	}

	// ── dataset ───────────────────────────────────────────────────────────────
	// Python: data=[{"data": {...}, "target": {...}}, ...]
	data := []laminar.DataPoint{
		{
			Data:   map[string]any{"country": "Canada", "capital": "Ottawa"},
			Target: map[string]any{"capital": "Ottawa"},
		},
	}

	// ── executor ──────────────────────────────────────────────────────────────
	// Python: executor=lambda data: data["capital"]
	executor := func(d map[string]any) (any, error) {
		return d["capital"], nil
	}

	// ── evaluators ────────────────────────────────────────────────────────────
	// Python: evaluators={"is_correct": lambda output, target: int(output == target["capital"])}
	evaluators := map[string]laminar.EvaluatorFunc{
		"is_correct": func(output any, target map[string]any) float64 {
			if output == target["capital"] {
				return 1
			}
			return 0
		},
	}

	// ── evaluate ──────────────────────────────────────────────────────────────
	err := laminar.Evaluate(context.Background(), laminar.Config{
		Data:          data,
		Executor:      executor,
		Evaluators:    evaluators,
		GroupName:     *groupName,
		ProjectAPIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("evaluate: %v", err)
	}

	fmt.Printf("✓ evaluation sent to Laminar (group: %s)\n", *groupName)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
