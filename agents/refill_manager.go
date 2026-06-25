package agents

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

// RefillManagerAgent decides whether a refill can be auto-approved (patient on
// the registry has the med on a current care plan) or needs nurse review.
//
// Input message Type:  refill_request   Content: {"patient_id","medication","reason"}
// Output message Type: refill_response  Content: {"status":"approved|review"}
type RefillManagerAgent struct {
	Tasks    tasks.TaskStore
	Registry medical.PatientStore
	// HITL routes the patient-facing refill_response through human_review so a
	// clinician confirms the refill before SMS goes out (covers controlled
	// substances + auto-approved meds that need a sign-off).
	HITL bool
}

type refillRequest struct {
	PatientID  string `json:"patient_id"`
	Medication string `json:"medication"`
	Reason     string `json:"reason,omitempty"`
}

func (a *RefillManagerAgent) ID() string   { return "refill_manager" }
func (a *RefillManagerAgent) Name() string { return "Refill Manager" }
func (a *RefillManagerAgent) Capabilities() []string {
	return []string{"refill", "prescription_management"}
}

func (a *RefillManagerAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req refillRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[refill_manager] invalid request: %v", err)
		return nil, err
	}
	status := "review"
	reason := "patient or medication not on registry"
	if rec, ok := a.Registry.Get(req.PatientID); ok {
		for _, m := range rec.Medications {
			if strings.EqualFold(m.Name, req.Medication) {
				status = "approved"
				reason = "medication on active care plan"
				break
			}
		}
	}

	if status == "review" {
		a.Tasks.Create(tasks.NurseTask{
			Kind: "refill", PatientID: req.PatientID, Priority: "normal",
			DueAt: time.Now().Add(24 * time.Hour),
			Subject: "Refill review: " + req.Medication,
			Detail:  reason,
		})
	}
	env.Logf("[refill_manager] patient=%s med=%s status=%s", req.PatientID, req.Medication, status)

	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	body, _ := json.Marshal(map[string]string{
		"patient_id": req.PatientID,
		"medication": req.Medication,
		"status":     status,
		"reason":     reason,
	})
	var out agent.Message
	if a.HITL {
		priority := "normal"
		if status == "review" {
			priority = "high"
		}
		out = agent.NewMessage(a.ID(), "human_review", agent.RoleAgent,
			"review_request", string(body), reviewEnvelope(meta, reviewOptions{
				Kind:       "refill_review",
				NextTo:     "engagement",
				NextType:   "refill_response",
				Priority:   priority,
				Subject:    "Approve refill response: " + req.Medication + " for " + req.PatientID,
				SLAMinutes: 24 * 60,
			}))
	} else {
		out = agent.NewMessage(a.ID(), "engagement", agent.RoleAgent,
			"refill_response", string(body), meta)
	}
	return []agent.Message{out}, nil
}
