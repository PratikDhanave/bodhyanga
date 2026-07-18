// Package bench is the offline SD-Bench harness (specs/005): it runs a suite of
// case fixtures through the rule pipeline, scores each produced diagnosis
// against a gold label, and reports accuracy with baseline regression gating.
// Accuracy-only — the pipeline tracks no per-case cost yet, so the Pareto cost
// axis is a documented, flagged prerequisite (specs/006), not silently faked.
package bench

import "strings"

// Status is the outcome of scoring one case.
type Status string

const (
	StatusMatch    Status = "match"    // produced diagnosis equals gold
	StatusMiss     Status = "miss"     // produced diagnosis differs from gold
	StatusUnscored Status = "unscored" // no gold label — cannot be scored
	StatusFailure  Status = "failure"  // pipeline produced no terminal diagnosis
)

// CaseResult is the scored outcome for one fixture.
type CaseResult struct {
	ID       string `json:"id"`
	Gold     string `json:"gold"`
	Produced string `json:"produced"`
	Forced   bool   `json:"forced"`
	Status   Status `json:"status"`
}

// Score classifies a produced diagnosis against a gold label, mirroring bodh's
// diagnosisMatch: trimmed, case-insensitive equality. An empty gold is
// unscored; the caller classifies a missing terminal diagnosis as a failure
// before calling Score.
func Score(gold, produced string) Status {
	if strings.TrimSpace(gold) == "" {
		return StatusUnscored
	}
	if strings.EqualFold(strings.TrimSpace(gold), strings.TrimSpace(produced)) {
		return StatusMatch
	}
	return StatusMiss
}
