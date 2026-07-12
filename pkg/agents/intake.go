package agents

import (
	"reflect"

	"github.com/microsoft/agent-framework-go/workflow"
)

// NewIntake builds the intake executor: it de-identifies the case and opens
// round 0. deidentify is the PHI seam (architecture.md §6) — step 2 wires
// bodh's pkg/phi here; nil means identity (tests, toy data).
func NewIntake(id string, deidentify func(PatientCase) PatientCase) *workflow.Executor {
	return workflow.NewExecutor(id, func(ctx *workflow.Context, pc PatientCase) error {
		if deidentify != nil {
			pc = deidentify(pc)
		}
		return ctx.SendMessage("", DiagnosticQuery{Case: pc, Round: 0})
	}).Extend(&workflow.Executor{
		ConfigureProtocol: func(rb *workflow.ProtocolBuilder) (*workflow.ProtocolBuilder, error) {
			rb.SendsMessageType(reflect.TypeFor[DiagnosticQuery]())
			return rb, nil
		},
	})
}
