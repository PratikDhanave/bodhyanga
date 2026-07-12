package agents

import (
	"reflect"
	"sort"

	"github.com/microsoft/agent-framework-go/workflow"
)

// testCatalog maps a symptom to the tests that work it up. Illustrative only
// (see NOTICE in bodh): real catalogs are a clinical-governance artifact.
var testCatalog = map[string][]string{
	"chest pain":          {"troponin", "ecg"},
	"fever":               {"cbc", "blood culture"},
	"cough":               {"chest xray"},
	"shortness of breath": {"bnp", "chest xray"},
}

// Lab produces a result for an ordered test. Injectable so tests and, later,
// real integrations can replace the simulation.
type Lab func(test string, c PatientCase) TestResult

// SimulatedLab returns the canonical abnormal finding for each known test —
// a toy world in which ordered tests confirm the underlying condition.
func SimulatedLab(test string, _ PatientCase) TestResult {
	values := map[string]string{
		"troponin":      "elevated",
		"ecg":           "st-elevation",
		"cbc":           "leukocytosis",
		"blood culture": "negative",
		"chest xray":    "infiltrate",
		"bnp":           "elevated",
	}
	v, ok := values[test]
	if !ok {
		v = "normal"
	}
	return TestResult{Name: test, Value: v}
}

// NewTestPlanner builds the planner executor. Round 0 sends the symptom-only
// workup (no tests); later rounds order catalog tests for the case's symptoms
// that have not been run yet.
func NewTestPlanner(id string, lab Lab, guard Guard) *workflow.Executor {
	if lab == nil {
		lab = SimulatedLab
	}
	return workflow.NewExecutor(id, func(ctx *workflow.Context, q DiagnosticQuery) error {
		return guarded(guard, ctx, id, "diagnostic_query", HashCaseID(q.Case.CaseID), func() error {
			var results []TestResult
			if q.Round > 0 {
				done := make(map[string]bool, len(q.PriorTests))
				for _, t := range q.PriorTests {
					done[t.Name] = true
				}
				var order []string
				for _, s := range q.Case.Symptoms {
					for _, test := range testCatalog[s] {
						if !done[test] {
							done[test] = true
							order = append(order, test)
						}
					}
				}
				sort.Strings(order) // deterministic runs → diffable audit trails
				for _, test := range order {
					results = append(results, lab(test, q.Case))
				}
			}
			return ctx.SendMessage("", Workup{Query: q, Results: append(q.PriorTests, results...)})
		})
	}).Extend(&workflow.Executor{
		ConfigureProtocol: func(rb *workflow.ProtocolBuilder) (*workflow.ProtocolBuilder, error) {
			rb.SendsMessageType(reflect.TypeFor[Workup]())
			return rb, nil
		},
	})
}
