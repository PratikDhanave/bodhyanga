package agents

import (
	"reflect"

	"github.com/microsoft/agent-framework-go/workflow"
)

// NewTranslator builds the opt-in front-door executor: it consumes a raw
// Inbound message, runs the injected translate function, and either emits one
// PatientCase (success) or halts the run with a PHI-free rejection (failure).
// It sits INSIDE the admission boundary so a denied translation hop does no
// work and emits no case (specs/002 FR-004).
//
// translate is injected (rather than importing pkg/intake) to keep the
// dependency one-way: intake → agents. On success the hop is keyed on the
// parsed case token; on a parse failure no token is known, so admission is
// keyed on the empty fingerprint and the guard records the rejection as an
// agent_error carrying only the reason category (FR-005/FR-006).
func NewTranslator(id string, translate func(Inbound) (PatientCase, error), guard Guard) *workflow.Executor {
	return workflow.NewExecutor(id, func(ctx *workflow.Context, in Inbound) error {
		pc, terr := translate(in)
		token := ""
		if terr == nil {
			token = HashCaseID(pc.CaseID)
		}
		// Admission runs regardless: a deny halts here (no case, deny record);
		// an allowed-but-rejected message returns terr, which the guard records
		// as agent_error and which fails the run (no PatientCase downstream).
		return guarded(guard, ctx, id, "inbound_message", token, func() error {
			if terr != nil {
				return terr
			}
			return ctx.SendMessage("", pc)
		})
	}).Extend(&workflow.Executor{
		ConfigureProtocol: func(rb *workflow.ProtocolBuilder) (*workflow.ProtocolBuilder, error) {
			rb.SendsMessageType(reflect.TypeFor[PatientCase]())
			return rb, nil
		},
	})
}
