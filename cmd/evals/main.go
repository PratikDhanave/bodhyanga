// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Command evals runs Bodh's multi-turn agent evaluation suite.
//
// # What it tests
//
// Multi-turn evals assess whether an LLM agent correctly accomplishes a task
// across multiple tool-call rounds — not just whether it picks the right first
// tool, but whether it uses tool results correctly, stops at the right time, and
// avoids unsafe tools.
//
// # How it works
//
//  1. Load a JSON dataset of (input, expected) pairs.
//  2. For each case run the agent in a mocked tool environment (no real I/O).
//  3. Apply three evaluators per case:
//     - toolOrder    : did tools fire in the expected sequence?
//     - toolsAvoided : were forbidden tools left uncalled?
//     - outputQuality: LLM-as-judge (semantic correctness, 0–1 scale)
//  4. Print a formatted results table and exit 1 if any case fails.
//
// # Environment variables
//
//	ANTHROPIC_API_KEY   — required; used for both the agent and judge
//	EVALS_DATASET       — path to JSON dataset (default: cmd/evals/data/agent-multiturn.json)
//	EVALS_AGENT_MODEL   — model for the agent under test (default: claude-haiku-4-5)
//	EVALS_JUDGE_MODEL   — model for the LLM judge (default: claude-sonnet-4-6)
//	EVALS_PASS_THRESHOLD— minimum overall score to pass (default: 0.6)
//	EVALS_OUTPUT        — path to write JSON scores (optional)
//	EVALS_NO_JUDGE      — set to "1" to skip the LLM judge (faster, cheaper)
//
// # Usage
//
//	go run ./cmd/evals/...
//	go run ./cmd/evals/... -dataset data/my-cases.json
//
// Exits 0 if all cases pass, 1 otherwise.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/PratikDhanave/bodh/pkg/evals/multiturn"
	"github.com/PratikDhanave/bodh/pkg/llm"
)

func main() {
	// Flags mirror environment variables; flags take precedence.
	datasetFlag := flag.String("dataset",
		envDefault("EVALS_DATASET", "cmd/evals/data/agent-multiturn.json"),
		"Path to the JSON eval dataset")
	agentModelFlag := flag.String("agent-model",
		envDefault("EVALS_AGENT_MODEL", "claude-haiku-4-5"),
		"Model to use for the agent under test")
	judgeModelFlag := flag.String("judge-model",
		envDefault("EVALS_JUDGE_MODEL", "claude-sonnet-4-6"),
		"Model to use for the LLM judge (stronger = better)")
	outputFlag := flag.String("output",
		envDefault("EVALS_OUTPUT", ""),
		"Optional path to write JSON scores")
	noJudgeFlag := flag.Bool("no-judge",
		envBool("EVALS_NO_JUDGE", false),
		"Skip LLM-as-judge (faster/cheaper; outputQuality scores 1.0)")
	thresholdFlag := flag.Float64("pass-threshold",
		envFloat("EVALS_PASS_THRESHOLD", 0.6),
		"Minimum overall score for a case to pass (0.0–1.0)")
	flag.Parse()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("evals: ANTHROPIC_API_KEY is required")
	}

	// Load dataset.
	cases, err := multiturn.LoadDataset(*datasetFlag)
	if err != nil {
		log.Fatalf("evals: load dataset: %v", err)
	}
	fmt.Printf("evals: loaded %d cases from %s\n", len(cases), *datasetFlag)
	fmt.Printf("evals: agent=%s judge=%s threshold=%.0f%%\n",
		*agentModelFlag, *judgeModelFlag, *thresholdFlag*100)

	// Build clients.
	agentClient := llm.NewAnthropic(apiKey, *agentModelFlag)

	var judgeClient llm.Client
	if !*noJudgeFlag {
		judgeClient = llm.NewAnthropic(apiKey, *judgeModelFlag)
	}

	// Run eval suite.
	runner := multiturn.NewRunner(multiturn.RunConfig{
		AgentClient:   agentClient,
		JudgeClient:   judgeClient,
		PassThreshold: *thresholdFlag,
	})

	ctx := context.Background()
	scores, allPass := runner.Run(ctx, cases)

	// Optionally save scores to JSON.
	if *outputFlag != "" {
		if err := multiturn.SaveScores(*outputFlag, scores); err != nil {
			log.Printf("evals: save scores: %v", err)
		} else {
			fmt.Printf("evals: scores written to %s\n", *outputFlag)
		}
	}

	if !allPass {
		fmt.Fprintf(os.Stderr, "\nevals: FAIL — one or more cases below threshold %.0f%%\n",
			*thresholdFlag*100)
		os.Exit(1)
	}
	fmt.Println("\nevals: PASS")
}

// envDefault returns the env var value or fallback.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool returns the env var as bool or the default.
func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// envFloat returns the env var as float64 or the default.
func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
