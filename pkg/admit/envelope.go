package admit

import (
	"fmt"
	"time"

	"github.com/PratikDhanave/bodh/pkg/protocol"
	"github.com/google/uuid"
)

// Message-type labels — the wire-level catalog governance and audit see.
// One label per payload type; an unlabeled type is a design error and is
// DENIED, not waved through (fail-closed, data-model.md).
const (
	TypePatientCase     = "patient_case"
	TypeDiagnosticQuery = "diagnostic_query"
	TypeWorkup          = "workup"
	TypeDiagnosisResult = "diagnosis_result"
	TypeCarePlan        = "care_plan"
)

var knownTypes = map[string]bool{
	TypePatientCase:     true,
	TypeDiagnosticQuery: true,
	TypeWorkup:          true,
	TypeDiagnosisResult: true,
	TypeCarePlan:        true,
}

// buildEnvelope synthesizes the PHI-free protocol.Message a hop presents to
// governance. Content is ALWAYS empty — payloads never reach policies or the
// audit trail; caseToken must already be the opaque fingerprint.
func buildEnvelope(agentID, msgType, caseToken string) (protocol.Message, error) {
	if !knownTypes[msgType] {
		return protocol.Message{}, fmt.Errorf("admit: unlabeled message type %q", msgType)
	}
	return protocol.Message{
		ID:        uuid.NewString(),
		To:        agentID,
		Role:      protocol.RoleAgent,
		Type:      msgType,
		Content:   "",
		CreatedAt: time.Now().UTC(),
		Metadata:  map[string]any{"case_id": caseToken},
	}, nil
}
