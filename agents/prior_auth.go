package agents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

// PriorAuthAgent tracks payer prior authorization requests.
//
// Input message Type:  prior_auth_request   Content: {"patient_id","code","payer"}
// Output message Type: prior_auth_status    Content: {"status":"submitted|approved|review"}
type PriorAuthAgent struct {
	Tasks *tasks.Queue
}

type priorAuthRequest struct {
	PatientID string `json:"patient_id"`
	Code      string `json:"code"`
	Payer     string `json:"payer,omitempty"`
}

func (a *PriorAuthAgent) ID() string   { return "prior_auth" }
func (a *PriorAuthAgent) Name() string { return "Prior Authorization Manager" }
func (a *PriorAuthAgent) Capabilities() []string {
	return []string{"prior_auth"}
}

var lowRiskAuthCodes = map[string]bool{
	"DEXA": true,
	"CT":   true,
}

func (a *PriorAuthAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req priorAuthRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[prior_auth] invalid request: %v", err)
		return nil, err
	}
	status := "submitted"
	if lowRiskAuthCodes[req.Code] {
		status = "approved"
	}
	if status != "approved" {
		a.Tasks.Create(tasks.NurseTask{
			Kind: "prior_auth", PatientID: req.PatientID, Priority: "high",
			DueAt: time.Now().Add(48 * time.Hour),
			Subject: "PA pending: " + req.Code,
			Detail:  "Payer=" + req.Payer,
		})
	}
	env.Logf("[prior_auth] patient=%s code=%s status=%s", req.PatientID, req.Code, status)

	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	body, _ := json.Marshal(map[string]string{
		"patient_id": req.PatientID,
		"code":       req.Code,
		"status":     status,
	})
	out := agent.NewMessage(a.ID(), "", agent.RoleAgent, "prior_auth_status",
		string(body), meta)
	return []agent.Message{out}, nil
}
