package agents

import (
	"reflect"

	"github.com/microsoft/agent-framework-go/workflow"
)

// planCatalog maps a diagnosis to care-plan steps. Illustrative only.
var planCatalog = map[string][]string{
	"acute coronary syndrome": {
		"activate cardiology consult",
		"aspirin per protocol",
		"serial troponin monitoring",
	},
	"community-acquired pneumonia": {
		"empiric antibiotics per antibiogram",
		"oxygen saturation monitoring",
		"repeat imaging in 48h",
	},
	"heart failure": {
		"diuresis per protocol",
		"daily weight monitoring",
		"echocardiogram",
	},
}

// NewCarePlan builds the terminal executor: DiagnosisResult → CarePlan,
// yielded as the workflow output. Forced (low-confidence) diagnoses always
// route to specialist review rather than a protocol plan.
func NewCarePlan(id string) *workflow.Executor {
	return workflow.NewExecutor(id, func(ctx *workflow.Context, d DiagnosisResult) error {
		steps, ok := planCatalog[d.Diagnosis]
		if !ok || d.Forced {
			steps = []string{"refer for specialist review", "schedule follow-up assessment"}
		}
		return ctx.YieldOutput(CarePlan{
			CaseID:    d.CaseID,
			Diagnosis: d.Diagnosis,
			Forced:    d.Forced,
			Steps:     steps,
		})
	}).Extend(&workflow.Executor{
		ConfigureProtocol: func(rb *workflow.ProtocolBuilder) (*workflow.ProtocolBuilder, error) {
			rb.YieldsOutputType(reflect.TypeFor[CarePlan]())
			return rb, nil
		},
	})
}
