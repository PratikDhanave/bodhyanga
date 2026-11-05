package admit

import (
	"fmt"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
	"github.com/google/uuid"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
)

// Message-type labels — the wire-level catalog governance and audit see.
// One label per payload type; an unlabeled type is a design error and is
// DENIED, not waved through (fail-closed, data-model.md).
const (
	TypeInboundMessage  = "inbound_message" // raw front-door message (specs/002)
	TypePatientCase     = "patient_case"
	TypeDiagnosticQuery = "diagnostic_query"
	TypeWorkup          = "workup"
	TypeDiagnosisResult = "diagnosis_result"
	TypeCarePlan        = "care_plan"
	TypeReviewDecision  = "review_decision"
)

var knownTypes = map[string]bool{
	TypeInboundMessage:  true,
	TypePatientCase:     true,
	TypeDiagnosticQuery: true,
	TypeWorkup:          true,
	TypeDiagnosisResult: true,
	TypeCarePlan:        true,
	TypeReviewDecision:  true,
}

// LabelFor maps a pipeline payload to its wire-level label — the single
// source of truth the audit bridge uses so trail rows and admission
// envelopes agree on type names.
func LabelFor(payload any) (string, bool) {
	switch payload.(type) {
	case agents.Inbound:
		return TypeInboundMessage, true
	case agents.PatientCase:
		return TypePatientCase, true
	case agents.DiagnosticQuery:
		return TypeDiagnosticQuery, true
	case agents.Workup:
		return TypeWorkup, true
	case agents.DiagnosisResult:
		return TypeDiagnosisResult, true
	case agents.CarePlan:
		return TypeCarePlan, true
	case agents.ReviewDecision:
		return TypeReviewDecision, true
	default:
		return "", false
	}
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
