package intake

import (
	"encoding/json"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

// Translate is the front-door dispatcher: it routes an Inbound to the decoder
// for its declared format and returns one PatientCase, or a PHI-free
// RejectError. It is injected into the translation executor
// (agents.NewTranslator) so pkg/agents never imports pkg/intake — the
// dependency runs one way, intake → agents (plan §import-cycle note).
func Translate(in agents.Inbound) (agents.PatientCase, error) {
	switch in.Format {
	case agents.FormatJSON:
		var pc agents.PatientCase
		if err := json.Unmarshal(in.Payload, &pc); err != nil {
			return agents.PatientCase{}, reject(ReasonMalformed)
		}
		return pc, nil
	case agents.FormatHL7:
		return translateHL7(in.Payload)
	case agents.FormatFHIR:
		return translateFHIR(in.Payload)
	default:
		return agents.PatientCase{}, reject(ReasonUnsupportedType)
	}
}
