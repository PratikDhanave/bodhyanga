package agents

import (
	"reflect"

	"github.com/microsoft/agent-framework-go/workflow"
)

// rule is one entry in the toy differential table: symptoms suggest a
// diagnosis; supporting test findings confirm it. Illustrative only.
type rule struct {
	diagnosis  string
	symptoms   []string
	supporting map[string]string // test name → confirming value
}

var rules = []rule{
	{
		diagnosis:  "acute coronary syndrome",
		symptoms:   []string{"chest pain"},
		supporting: map[string]string{"troponin": "elevated", "ecg": "st-elevation"},
	},
	{
		diagnosis:  "community-acquired pneumonia",
		symptoms:   []string{"fever", "cough"},
		supporting: map[string]string{"chest xray": "infiltrate", "cbc": "leukocytosis"},
	},
	{
		diagnosis:  "heart failure",
		symptoms:   []string{"shortness of breath"},
		supporting: map[string]string{"bnp": "elevated"},
	},
}

// ForcedProgressionEvent is the domain event emitted when the max-round guard
// forces a case forward below the confidence bar. Carries the hashed case ID
// only (PHI rule).
type ForcedProgressionEvent struct {
	CaseIDHash string
	Confidence float64
	Rounds     int
}

// Data implements workflow.Event.
func (e ForcedProgressionEvent) Data() any { return e }

// DiagnosticianConfig tunes the loop-back guard (architecture.md §6).
type DiagnosticianConfig struct {
	Tau       float64 // confidence bar; proceed to care plan above it
	MaxRounds int     // forced progression at this many workups
}

// NewRuleDiagnostician builds the rule-path diagnostician executor. It sends
// DiagnosisResult when confident (or forced) and DiagnosticQuery to loop back
// otherwise — the graph's conditional edges route on those types.
func NewRuleDiagnostician(id string, cfg DiagnosticianConfig, guard Guard) *workflow.Executor {
	return workflow.NewExecutor(id, func(ctx *workflow.Context, wu Workup) error {
		return guarded(guard, ctx, id, "workup", HashCaseID(wu.Query.Case.CaseID), func() error {
			return diagnose(ctx, cfg, wu)
		})
	}).Extend(&workflow.Executor{
		ConfigureProtocol: func(rb *workflow.ProtocolBuilder) (*workflow.ProtocolBuilder, error) {
			rb.SendsMessageType(reflect.TypeFor[DiagnosisResult]())
			rb.SendsMessageType(reflect.TypeFor[DiagnosticQuery]())
			return rb, nil
		},
	})
}

// diagnose is the rule path's per-workup body.
func diagnose(ctx *workflow.Context, cfg DiagnosticianConfig, wu Workup) error {
	diagnosis, confidence, evidence := evaluateRules(wu)
	return routeDiagnosis(ctx, cfg, wu, diagnosis, confidence, evidence)
}

// routeDiagnosis applies the loop-back guard shared by ALL diagnostician
// engines (spec FR-010): confident → DiagnosisResult; round budget exhausted
// → forced DiagnosisResult + domain event; otherwise → loop back to the
// planner with accumulated results.
func routeDiagnosis(ctx *workflow.Context, cfg DiagnosticianConfig, wu Workup, diagnosis string, confidence float64, evidence []string) error {
	rounds := wu.Query.Round + 1

	if confidence > cfg.Tau {
		return ctx.SendMessage("", DiagnosisResult{
			CaseID:     wu.Query.Case.CaseID,
			Diagnosis:  diagnosis,
			Confidence: confidence,
			Rounds:     rounds,
			Evidence:   evidence,
		})
	}
	if rounds >= cfg.MaxRounds {
		ctx.AddEvent(ForcedProgressionEvent{
			CaseIDHash: HashCaseID(wu.Query.Case.CaseID),
			Confidence: confidence,
			Rounds:     rounds,
		})
		return ctx.SendMessage("", DiagnosisResult{
			CaseID:     wu.Query.Case.CaseID,
			Diagnosis:  diagnosis,
			Confidence: confidence,
			Rounds:     rounds,
			Forced:     true,
			Evidence:   evidence,
		})
	}
	return ctx.SendMessage("", DiagnosticQuery{
		Case:       wu.Query.Case,
		Round:      wu.Query.Round + 1,
		PriorTests: wu.Results,
	})
}

// evaluateRules scores every rule against the workup and returns the best.
// Confidence = 0.55·(symptom match fraction) + 0.25 per confirming test,
// capped at 0.97: symptoms alone never clear τ=0.85, one confirming test may
// not either, symptoms + strong test evidence does.
func evaluateRules(wu Workup) (diagnosis string, confidence float64, evidence []string) {
	symptoms := make(map[string]bool, len(wu.Query.Case.Symptoms))
	for _, s := range wu.Query.Case.Symptoms {
		symptoms[s] = true
	}
	results := make(map[string]string, len(wu.Results))
	for _, r := range wu.Results {
		results[r.Name] = r.Value
	}

	diagnosis, confidence = "undifferentiated presentation", 0.30
	for _, r := range rules {
		matched := 0
		for _, s := range r.symptoms {
			if symptoms[s] {
				matched++
			}
		}
		if matched == 0 {
			continue
		}
		score := 0.55 * float64(matched) / float64(len(r.symptoms))
		var ev []string
		for test, confirming := range r.supporting {
			if results[test] == confirming {
				score += 0.25
				ev = append(ev, test+"="+confirming)
			}
		}
		if score > 0.97 {
			score = 0.97
		}
		if score > confidence {
			diagnosis, confidence, evidence = r.diagnosis, score, ev
		}
	}
	return diagnosis, confidence, evidence
}
