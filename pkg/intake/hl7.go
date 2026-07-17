package intake

import (
	"github.com/PratikDhanave/bodh/pkg/hl7v2"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

// translateHL7 turns an HL7 v2 ADT message into one PatientCase. The parse and
// ADT-conversion logic is bodh's pkg/hl7v2, imported UNCHANGED; this seam only
// maps medical.ADTEvent → agents.PatientCase and categorizes failures into the
// PHI-free RejectReason taxonomy (never wrapping the parser's raw error text).
//
// The mapping is deliberately demo-grade: an ADT feed carries a diagnosis and
// identifiers, not a symptom workup, so History is seeded from the primary
// diagnosis and Symptoms is left empty (an undifferentiated case is a designed
// outcome — spec FR-008).
func translateHL7(payload []byte) (agents.PatientCase, error) {
	msg, err := hl7v2.ParseMessage(string(payload))
	if err != nil {
		return agents.PatientCase{}, reject(ReasonMalformed)
	}
	// Wrong message class (e.g. an ORU result feed) — parseable, not an ADT.
	if msg.Header.MessageType != "ADT" {
		return agents.PatientCase{}, reject(ReasonUnsupportedType)
	}
	// Required identity segment absent.
	if _, ok := msg.FindFirst("PID"); !ok {
		return agents.PatientCase{}, reject(ReasonMissingSegment)
	}
	ev, err := hl7v2.MessageToADTEvent(msg)
	if err != nil {
		// ADT with a PID, but conversion still failed → an unhandled trigger
		// event (or an unusable PID-3 / timestamp). The category is PHI-free
		// regardless of which; treat as an unsupported trigger.
		return agents.PatientCase{}, reject(ReasonUnsupportedTrigger)
	}
	pc := agents.PatientCase{CaseID: ev.PatientID}
	if ev.PrimaryDx != "" {
		pc.History = []string{ev.PrimaryDx}
	}
	return pc, nil
}
