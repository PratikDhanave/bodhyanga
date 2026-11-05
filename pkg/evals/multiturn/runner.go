// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package multiturn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

// RunConfig controls how Runner executes the eval suite.
type RunConfig struct {
	// AgentClient is the LLM used to run the agent being evaluated.
	AgentClient llm.Client

	// JudgeClient is the LLM used for LLM-as-judge scoring. Should be a
	// strong model (e.g. claude-opus or claude-sonnet with high quota).
	// If nil, OutputQuality is skipped and scores 1.0 automatically.
	JudgeClient llm.Client

	// PassThreshold is the minimum Overall score to consider a case passing.
	// Default 0.6 (60%).
	PassThreshold float64

	// Output is where the runner prints its report. Defaults to os.Stdout.
	Output io.Writer
}

// Runner executes a multi-turn eval dataset and reports scores.
type Runner struct {
	cfg RunConfig
}

// NewRunner creates a Runner with the given configuration.
func NewRunner(cfg RunConfig) *Runner {
	if cfg.PassThreshold == 0 {
		cfg.PassThreshold = 0.6
	}
	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}
	return &Runner{cfg: cfg}
}

// LoadDataset reads eval cases from a JSON file.
// The file must contain a JSON array of EvalCase objects.
func LoadDataset(path string) ([]EvalCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("multiturn: open dataset %q: %w", path, err)
	}
	defer f.Close()

	var cases []EvalCase
	if err := json.NewDecoder(f).Decode(&cases); err != nil {
		return nil, fmt.Errorf("multiturn: decode dataset %q: %w", path, err)
	}
	return cases, nil
}

// Run executes all cases in the dataset and returns per-case scores.
// It prints a formatted table to cfg.Output and returns true if every case
// scored at or above cfg.PassThreshold.
func (r *Runner) Run(ctx context.Context, cases []EvalCase) ([]EvalScore, bool) {
	scores := make([]EvalScore, 0, len(cases))

	for i, c := range cases {
		score := r.runOne(ctx, i, c)
		scores = append(scores, score)
	}

	r.printReport(cases, scores)

	// All cases must pass.
	allPass := true
	for _, s := range scores {
		if s.Error != "" || s.Overall < r.cfg.PassThreshold {
			allPass = false
			break
		}
	}
	return scores, allPass
}

// runOne executes a single eval case and returns its score.
func (r *Runner) runOne(ctx context.Context, idx int, c EvalCase) EvalScore {
	score := EvalScore{CaseIndex: idx, ToolOrder: 1.0, ToolsAvoided: 1.0, OutputQuality: 1.0}

	result, err := MultiTurnWithMocks(ctx, r.cfg.AgentClient, c.Data)
	if err != nil {
		score.Error = err.Error()
		score.Overall = 0
		return score
	}
	score.Result = result

	// Evaluator 1: tool order.
	score.ToolOrder = ToolOrderCorrect(result, &c.Target)

	// Evaluator 2: forbidden tools avoided.
	score.ToolsAvoided = ToolsAvoided(result, &c.Target)

	// Evaluator 3: LLM-as-judge (skip if no judge client).
	if r.cfg.JudgeClient != nil {
		q, err := LLMJudge(ctx, r.cfg.JudgeClient, result, &c.Target)
		if err != nil {
			// Judge failure doesn't fail the case; log and use 0.
			fmt.Fprintf(r.cfg.Output, "[warn] case %d: judge error: %v\n", idx, err)
			q = 0
		}
		score.OutputQuality = q
	}

	score.Overall = (score.ToolOrder + score.ToolsAvoided + score.OutputQuality) / 3.0
	return score
}

// printReport writes a formatted summary table to cfg.Output.
func (r *Runner) printReport(cases []EvalCase, scores []EvalScore) {
	w := tabwriter.NewWriter(r.cfg.Output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tTask\tToolOrder\tAvoided\tQuality\tOverall\tPass")
	fmt.Fprintln(w, "-\t----\t---------\t-------\t-------\t-------\t----")

	var totalOverall float64
	pass, fail := 0, 0

	for i, s := range scores {
		task := cases[i].Target.OriginalTask
		if len(task) > 40 {
			task = task[:37] + "..."
		}
		passStr := "✓"
		if s.Error != "" || s.Overall < r.cfg.PassThreshold {
			passStr = "✗"
			fail++
		} else {
			pass++
		}
		errStr := ""
		if s.Error != "" {
			errStr = " [err: " + s.Error + "]"
		}
		fmt.Fprintf(w, "%d\t%s\t%.2f\t%.2f\t%.2f\t%.2f\t%s%s\n",
			i,
			task,
			s.ToolOrder,
			s.ToolsAvoided,
			s.OutputQuality,
			s.Overall,
			passStr,
			errStr,
		)
		totalOverall += s.Overall
	}

	fmt.Fprintln(w)
	n := len(scores)
	if n > 0 {
		fmt.Fprintf(w, "Average\t\t\t\t\t%.2f\t%d/%d pass\n",
			totalOverall/float64(n), pass, n)
	}
	_ = w.Flush()
}

// SaveScores writes scores as a JSON array to path.
func SaveScores(path string, scores []EvalScore) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(scores)
}
