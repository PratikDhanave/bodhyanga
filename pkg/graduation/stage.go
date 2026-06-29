// Package graduation models the autonomy lifecycle an agent climbs before it
// is allowed to act on patients unsupervised, and the statistical gate that
// governs each promotion.
//
// The ladder (decoupling Phase 1):
//
//		development → shadow → copilot → autopilot
//
//	  - development: not wired into any clinical flow; offline only.
//	  - shadow:      runs in parallel with clinicians, records concordance, has
//	                 ZERO patient impact (see pkg/shadow).
//	  - copilot:     suggests, a human approves every action (the HITL gate is
//	                 mandatory at this stage).
//	  - autopilot:   acts autonomously within its governed scope.
//
// Promotion is never time-based; it is earned by clearing a Wilson-score lower
// bound on the agent's success rate over a minimum number of observations
// (see wilson.go and graduation.go). Demotion (including an operator kill via
// pkg/governance/agentmesh) can drop an agent any number of rungs at once;
// only promotion is one-rung-at-a-time and evidence-gated.
package graduation

import "fmt"

// Stage is an agent's autonomy level. It is a string (not an int) so it
// round-trips cleanly through JSON manifests, audit events, and HTTP without a
// lookup table, and so an unknown value is inspectable rather than a silent 0.
type Stage string

const (
	StageDevelopment Stage = "development"
	StageShadow      Stage = "shadow"
	StageCopilot     Stage = "copilot"
	StageAutopilot   Stage = "autopilot"
)

// ladder is the canonical ordering; index = autonomy rank.
var ladder = []Stage{StageDevelopment, StageShadow, StageCopilot, StageAutopilot}

// Valid reports whether s is one of the four defined stages.
func (s Stage) Valid() bool { return s.Rank() >= 0 }

// Rank returns the autonomy rank (0=development … 3=autopilot), or -1 if s is
// not a defined stage.
func (s Stage) Rank() int {
	for i, v := range ladder {
		if v == s {
			return i
		}
	}
	return -1
}

// Next returns the stage one rung up and ok=true, or ok=false if s is autopilot
// (terminal) or invalid. Promotion is always exactly one rung.
func (s Stage) Next() (Stage, bool) {
	r := s.Rank()
	if r < 0 || r >= len(ladder)-1 {
		return s, false
	}
	return ladder[r+1], true
}

// Prev returns the stage one rung down and ok=true, or ok=false if s is
// development (terminal) or invalid. (Demotion may skip rungs; Prev is the
// single-step helper.)
func (s Stage) Prev() (Stage, bool) {
	r := s.Rank()
	if r <= 0 {
		return s, false
	}
	return ladder[r-1], true
}

// ParseStage validates and returns a Stage, or an error for unknown input.
func ParseStage(s string) (Stage, error) {
	st := Stage(s)
	if !st.Valid() {
		return "", fmt.Errorf("graduation: unknown stage %q (want development|shadow|copilot|autopilot)", s)
	}
	return st, nil
}
