package agents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// ReadmissionTrackerAgent consumes ADT events, updates the patient registry,
// scores readmission risk, and on discharge triggers the TCM coordinator.
//
// Input message Type:  adt_event           Content: medical.ADTEvent JSON
// Output message Type: readmission_risk    Content: {patient_id, score, band}
// Output message Type: discharge_event     Content: medical.DischargeSummary JSON
type ReadmissionTrackerAgent struct {
	Registry *medical.PatientRegistry
}

func (a *ReadmissionTrackerAgent) ID() string   { return "readmission_tracker" }
func (a *ReadmissionTrackerAgent) Name() string { return "Readmission Risk Tracker" }
func (a *ReadmissionTrackerAgent) Capabilities() []string {
	return []string{"adt_ingest", "readmission_risk"}
}

func (a *ReadmissionTrackerAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	e, err := medical.DecodeADT(msg.Content)
	if err != nil {
		env.Logf("[readmission_tracker] invalid ADT: %v", err)
		return nil, err
	}

	rec, ok := a.Registry.Get(e.PatientID)
	if !ok {
		rec = medical.PatientRecord{PatientID: e.PatientID}
	}
	if e.Type == medical.ADTDischarge {
		rec.DischargedAt = e.OccurredAt
	}
	a.Registry.Upsert(rec)

	risk := medical.ScoreReadmissionRisk(rec, time.Now())
	env.Logf("[readmission_tracker] patient=%s %s risk=%.2f band=%s",
		e.PatientID, e.Type, risk.Score, risk.Band)

	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = e.PatientID
	riskBody, _ := json.Marshal(map[string]any{
		"patient_id": e.PatientID,
		"score":      risk.Score,
		"band":       risk.Band,
		"event":      e.Type,
	})
	out := []agent.Message{
		agent.NewMessage(a.ID(), "", agent.RoleAgent, "readmission_risk",
			string(riskBody), meta),
	}

	if e.Type == medical.ADTDischarge {
		d := medical.DischargeSummary{
			CaseID:       e.EventID,
			PatientID:    e.PatientID,
			DischargedAt: e.OccurredAt,
			PrimaryDx:    e.PrimaryDx,
			Disposition:  "home",
			HighRisk:     risk.Band == "high",
		}
		dMeta := copyMeta(meta)
		dMeta["case_id"] = e.EventID
		out = append(out, agent.NewMessage(a.ID(), "tcm_coordinator",
			agent.RoleAgent, "discharge_event", medical.EncodeDischarge(d), dMeta))
	}
	return out, nil
}
