package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// PreVisitPrepAgent assembles a chart summary before a clinician sees the patient.
// Input message Type:  pre_visit_request   Content: {"patient_id","encounter_id"}
// Output message Type: pre_visit_ready     Content: prep summary JSON
type PreVisitPrepAgent struct {
	Registry *medical.PatientRegistry
}

type preVisitRequest struct {
	PatientID   string `json:"patient_id"`
	EncounterID string `json:"encounter_id,omitempty"`
}

type preVisitSummary struct {
	PatientID    string   `json:"patient_id"`
	EncounterID  string   `json:"encounter_id,omitempty"`
	Highlights   []string `json:"highlights"`
	OpenGaps     []string `json:"open_gaps,omitempty"`
	SuggestedDx  []string `json:"suggested_diagnoses,omitempty"`
	Headline     string   `json:"headline"`
}

func (a *PreVisitPrepAgent) ID() string   { return "pre_visit_prep" }
func (a *PreVisitPrepAgent) Name() string { return "Pre-Visit Chart Prep" }
func (a *PreVisitPrepAgent) Capabilities() []string {
	return []string{"pre_visit_prep", "chart_summary"}
}

func (a *PreVisitPrepAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req preVisitRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[pre_visit_prep] invalid request: %v", err)
		return nil, err
	}
	rec, ok := a.Registry.Get(req.PatientID)
	if !ok {
		env.Logf("[pre_visit_prep] unknown patient %s", req.PatientID)
		return nil, nil
	}

	sum := preVisitSummary{
		PatientID:   req.PatientID,
		EncounterID: req.EncounterID,
		SuggestedDx: rec.Conditions,
	}
	if rec.LastSystolicBP > 0 {
		sum.Highlights = append(sum.Highlights,
			fmt.Sprintf("Last SBP %.0f mmHg on %s", rec.LastSystolicBP, rec.LastBPDate.Format("2006-01-02")))
	}
	if rec.LastA1c > 0 {
		sum.Highlights = append(sum.Highlights,
			fmt.Sprintf("Last A1c %.1f%% on %s", rec.LastA1c, rec.LastA1cDate.Format("2006-01-02")))
	}
	if rec.LastWeightLb > 0 {
		sum.Highlights = append(sum.Highlights, fmt.Sprintf("Weight %.0f lb", rec.LastWeightLb))
	}
	if len(rec.Medications) > 0 {
		names := make([]string, 0, len(rec.Medications))
		for _, m := range rec.Medications {
			names = append(names, m.Name)
		}
		sum.Highlights = append(sum.Highlights, "Meds: "+strings.Join(names, ", "))
	}
	for _, c := range rec.Conditions {
		sum.OpenGaps = append(sum.OpenGaps, "review_"+c)
	}
	sum.Headline = fmt.Sprintf("%d highlights, %d open items", len(sum.Highlights), len(sum.OpenGaps))
	env.Logf("[pre_visit_prep] patient=%s %s", req.PatientID, sum.Headline)

	body, _ := json.Marshal(sum)
	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	out := agent.NewMessage(a.ID(), "", agent.RoleAgent, "pre_visit_ready", string(body), meta)
	return []agent.Message{out}, nil
}
