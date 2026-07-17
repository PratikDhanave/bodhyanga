package intake

import "github.com/PratikDhanave/bodh-maf/pkg/agents"

// translateFHIR decodes a FHIR R4 bundle into one PatientCase.
//
// TODO(specs/002 US2, T013): implement the minimal Bundle decoder + the
// bodh/pkg/fhir resource mapping. Until then FHIR input is rejected as an
// unsupported type rather than silently accepted — fail loud, never fabricate.
func translateFHIR(_ []byte) (agents.PatientCase, error) {
	return agents.PatientCase{}, reject(ReasonUnsupportedType)
}
