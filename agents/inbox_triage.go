package agents

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/tasks"
)

// InboxTriageAgent classifies inbound patient messages and either auto-routes
// them (refills, prior-auth) or queues a nurse task.
//
// Input message Type:  inbox_message   Content: {"patient_id","subject","body"}
// Output message Type: refill_request | prior_auth_request | inbox_routed
type InboxTriageAgent struct {
	Tasks *tasks.Queue
}

type inboxMessage struct {
	PatientID string `json:"patient_id"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Channel   string `json:"channel,omitempty"`
}

func (a *InboxTriageAgent) ID() string   { return "inbox_triage" }
func (a *InboxTriageAgent) Name() string { return "Clinical Inbox Triage" }
func (a *InboxTriageAgent) Capabilities() []string {
	return []string{"inbox_triage", "message_routing"}
}

func (a *InboxTriageAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var item inboxMessage
	if err := json.Unmarshal([]byte(msg.Content), &item); err != nil {
		env.Logf("[inbox_triage] invalid message: %v", err)
		return nil, err
	}
	category := classifyInbox(item.Subject, item.Body)
	env.Logf("[inbox_triage] patient=%s category=%s subject=%q", item.PatientID, category, item.Subject)

	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = item.PatientID
	meta["inbox_category"] = category

	switch category {
	case "refill":
		body, _ := json.Marshal(map[string]string{
			"patient_id": item.PatientID,
			"medication": extractMedication(item.Body),
			"reason":     "patient inbox",
		})
		return []agent.Message{agent.NewMessage(a.ID(), "refill_manager",
			agent.RoleAgent, "refill_request", string(body), meta)}, nil
	case "prior_auth":
		body, _ := json.Marshal(map[string]string{
			"patient_id": item.PatientID,
			"code":       extractCode(item.Body),
		})
		return []agent.Message{agent.NewMessage(a.ID(), "prior_auth",
			agent.RoleAgent, "prior_auth_request", string(body), meta)}, nil
	case "urgent_clinical":
		a.Tasks.Create(tasks.NurseTask{
			Kind: "inbox", PatientID: item.PatientID, Priority: "urgent",
			DueAt: time.Now().Add(2 * time.Hour),
			Subject: "Urgent inbox: " + item.Subject,
			Detail:  item.Body,
		})
	default:
		a.Tasks.Create(tasks.NurseTask{
			Kind: "inbox", PatientID: item.PatientID, Priority: "normal",
			DueAt: time.Now().Add(24 * time.Hour),
			Subject: item.Subject, Detail: item.Body,
		})
	}

	out := agent.NewMessage(a.ID(), "", agent.RoleAgent, "inbox_routed",
		`{"category":"`+category+`"}`, meta)
	return []agent.Message{out}, nil
}

func classifyInbox(subject, body string) string {
	s := strings.ToLower(subject + " " + body)
	switch {
	case strings.Contains(s, "refill") || strings.Contains(s, "prescription"):
		return "refill"
	case strings.Contains(s, "prior auth") || strings.Contains(s, "authorization"):
		return "prior_auth"
	case strings.Contains(s, "chest pain") || strings.Contains(s, "shortness of breath") ||
		strings.Contains(s, "bleeding") || strings.Contains(s, "syncope"):
		return "urgent_clinical"
	default:
		return "general"
	}
}

func extractMedication(body string) string {
	body = strings.ToLower(body)
	for _, m := range []string{"metformin", "lisinopril", "atorvastatin", "furosemide", "tiotropium", "losartan", "albuterol"} {
		if strings.Contains(body, m) {
			return m
		}
	}
	return "unknown"
}

func extractCode(body string) string {
	for _, code := range []string{"MRI", "CT", "PET", "DEXA"} {
		if strings.Contains(strings.ToUpper(body), code) {
			return code
		}
	}
	return "unknown"
}
