// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Command evals runs Bodhyanga's multi-turn agent evaluation suite.
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
// # Default provider: Ollama (local, no API key required)
//
// Out of the box the eval suite runs against a local Ollama instance.
// Make sure Ollama is running and the target model is pulled:
//
//	ollama serve                          # start if not already running
//	ollama pull qwen2.5:7b               # default agent model
//	go run ./cmd/evals/...
//
// # Switching to Anthropic
//
//	EVALS_PROVIDER=anthropic ANTHROPIC_API_KEY=sk-ant-... go run ./cmd/evals/...
//
// # Environment variables
//
//	EVALS_PROVIDER      — "ollama" (default) or "anthropic"
//	OLLAMA_HOST         — Ollama base URL (default: http://localhost:11434)
//	ANTHROPIC_API_KEY   — required only when EVALS_PROVIDER=anthropic
//	EVALS_DATASET       — path to JSON dataset (default: cmd/evals/data/agent-multiturn.json)
//	EVALS_AGENT_MODEL   — model for the agent (default: qwen2.5:7b for ollama, claude-haiku-4-5 for anthropic)
//	EVALS_JUDGE_MODEL   — model for the LLM judge (default: same as agent model)
//	EVALS_PASS_THRESHOLD— minimum overall score to pass (default: 0.6)
//	EVALS_OUTPUT        — path to write JSON scores (optional)
//	EVALS_NO_JUDGE      — set to "1" to skip the LLM judge (faster; outputQuality scores 1.0)
//
// # Usage
//
//	go run ./cmd/evals/...
//	go run ./cmd/evals/... -provider anthropic
//	go run ./cmd/evals/... -dataset data/my-cases.json -no-judge
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

	"github.com/PratikDhanave/bodhyanga/pkg/evals/multiturn"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

func main() {
	providerFlag := flag.String("provider",
		envDefault("EVALS_PROVIDER", "ollama"),
		`LLM provider: "ollama" (default, no API key needed) or "anthropic"`)
	datasetFlag := flag.String("dataset",
		envDefault("EVALS_DATASET", "cmd/evals/data/agent-multiturn.json"),
		"Path to the JSON eval dataset")
	agentModelFlag := flag.String("agent-model",
		envDefault("EVALS_AGENT_MODEL", ""),
		"Model for the agent under test (defaults to provider default)")
	judgeModelFlag := flag.String("judge-model",
		envDefault("EVALS_JUDGE_MODEL", ""),
		"Model for the LLM judge (defaults to agent model)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	outputFlag := flag.String("output",
		envDefault("EVALS_OUTPUT", ""),
		"Optional path to write JSON scores")
	noJudgeFlag := flag.Bool("no-judge",
		envBool("EVALS_NO_JUDGE", false),
		"Skip LLM-as-judge (outputQuality scores 1.0 automatically)")
	thresholdFlag := flag.Float64("pass-threshold",
		envFloat("EVALS_PASS_THRESHOLD", 0.6),
		"Minimum overall score for a case to pass (0.0–1.0)")
	flag.Parse()

	// Resolve model defaults per provider.
	agentModel, judgeModel := resolveModels(*providerFlag, *agentModelFlag, *judgeModelFlag)

	// Build agent + judge clients.
	agentClient, err := buildClient(*providerFlag, agentModel, *ollamaHostFlag)
	if err != nil {
		log.Fatalf("evals: agent client: %v", err)
	}

	var judgeClient llm.Client
	if !*noJudgeFlag {
		judgeClient, err = buildClient(*providerFlag, judgeModel, *ollamaHostFlag)
		if err != nil {
			log.Fatalf("evals: judge client: %v", err)
		}
	}

	// Load dataset.
	cases, err := multiturn.LoadDataset(*datasetFlag)
	if err != nil {
		log.Fatalf("evals: load dataset: %v", err)
	}

	judgeDesc := "skipped"
	if !*noJudgeFlag {
		judgeDesc = fmt.Sprintf("%s/%s", *providerFlag, judgeModel)
	}
	fmt.Printf("evals: loaded %d cases from %s\n", len(cases), *datasetFlag)
	fmt.Printf("evals: provider=%s  agent=%s  judge=%s  threshold=%.0f%%\n",
		*providerFlag, agentModel, judgeDesc, *thresholdFlag*100)

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

// resolveModels fills in default model names when the user didn't specify them.
// Judge defaults to the agent model when unset — using the same local model
// for both keeps the Ollama workflow self-contained.
func resolveModels(provider, agentModel, judgeModel string) (string, string) {
	if agentModel == "" {
		switch provider {
		case "anthropic":
			agentModel = "claude-haiku-4-5"
		default: // ollama
			agentModel = "qwen2.5:7b"
		}
	}
	if judgeModel == "" {
		switch provider {
		case "anthropic":
			// Use a stronger model as judge when running against Anthropic.
			judgeModel = "claude-sonnet-4-6"
		default: // ollama
			// Same model for agent and judge keeps it fully local.
			judgeModel = agentModel
		}
	}
	return agentModel, judgeModel
}

// buildClient constructs an llm.Client for the given provider.
// Returns an error if required credentials are missing.
func buildClient(provider, model, ollamaHost string) (llm.Client, error) {
	switch provider {
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY must be set when provider=anthropic")
		}
		return llm.NewAnthropic(apiKey, model), nil
	case "ollama":
		return llm.NewOllama(ollamaHost, model), nil
	default:
		return nil, fmt.Errorf("unknown provider %q — use \"ollama\" or \"anthropic\"", provider)
	}
}

// envDefault returns the env var value or the fallback string.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool parses a boolean env var, returning def when missing or unparseable.
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

// envFloat parses a float64 env var, returning def when missing or unparseable.
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
