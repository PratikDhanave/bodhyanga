package agents

import (
	"reflect"

	"github.com/microsoft/agent-framework-go/workflow"
)

// NewIntake builds the intake executor: it de-identifies the case and opens
// round 0. deidentify is the PHI seam (architecture.md §6); nil means
// identity (tests, toy data). De-identification runs BEFORE the guard so the
// admission envelope and every audit event key on the post-de-id token — one
// coherent case fingerprint across all hops.
func NewIntake(id string, deidentify func(PatientCase) PatientCase, guard Guard) *workflow.Executor {
	return workflow.NewExecutor(id, func(ctx *workflow.Context, pc PatientCase) error {
		if deidentify != nil {
			pc = deidentify(pc)
		}
		return guarded(guard, ctx, id, "patient_case", HashCaseID(pc.CaseID), func() error {
			return ctx.SendMessage("", DiagnosticQuery{Case: pc, Round: 0})
		})
	}).Extend(&workflow.Executor{
		ConfigureProtocol: func(rb *workflow.ProtocolBuilder) (*workflow.ProtocolBuilder, error) {
			rb.SendsMessageType(reflect.TypeFor[DiagnosticQuery]())
			return rb, nil
		},
	})
}
