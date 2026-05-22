package agents

import (
	"context"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// HumanReviewAgent is the platform's human-in-the-loop (HITL) gate. Clinical
// agents that need oversight emit a message of type "review_request" with
// HITL envelope fields in Metadata; this agent records the request in the
// queue and emits no follow-ups. When a reviewer decides via ApplyDecision,
// the original payload is resumed onto the bus toward its intended recipient.
//
// Envelope metadata keys set by the upstream agent:
//
//	hitl_kind         string  (required) e.g. "diagnosis_review", "care_plan_review"
//	hitl_next_to      string  (required) agent id to forward to on approval
//	hitl_next_type    string  (required) message type to use on resume
//	hitl_priority     string  (optional) low|normal|high|urgent (default normal)
//	hitl_subject      string  (optional) short label shown in the queue
//	hitl_sla_minutes  float64 (optional) deadline in minutes; expired = never resume
type HumanReviewAgent struct {
	Queue *hitl.Queue
}

func (a *HumanReviewAgent) ID() string   { return "human_review" }
func (a *HumanReviewAgent) Name() string { return "Human-in-the-Loop Reviewer" }
func (a *HumanReviewAgent) Capabilities() []string {
	return []string{"human_review", "clinical_oversight"}
}

// envelopeKeys are stripped from Metadata before the resumed message is
// republished, so downstream agents never see HITL bookkeeping.
var envelopeKeys = []string{
	"hitl_kind", "hitl_next_to", "hitl_next_type",
	"hitl_priority", "hitl_subject", "hitl_sla_minutes",
}

func (a *HumanReviewAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	kind := metadataString(msg.Metadata, "hitl_kind")
	nextTo := metadataString(msg.Metadata, "hitl_next_to")
	nextType := metadataString(msg.Metadata, "hitl_next_type")
	if kind == "" || nextTo == "" || nextType == "" {
		env.Logf("[human_review] envelope missing kind/next_to/next_type; dropping %s", msg.ID)
		return nil, nil
	}
	subject := metadataString(msg.Metadata, "hitl_subject")
	if subject == "" {
		subject = kind
	}
	priority := metadataString(msg.Metadata, "hitl_priority")
	if priority == "" {
		priority = "normal"
	}
	var sla time.Time
	if mins := metadataFloat(msg.Metadata, "hitl_sla_minutes", 0); mins > 0 {
		sla = time.Now().Add(time.Duration(mins) * time.Minute)
	}
	forwardMeta := copyMeta(msg.Metadata)
	for _, k := range envelopeKeys {
		delete(forwardMeta, k)
	}
	req := a.Queue.Submit(medical.ReviewRequest{
		Kind:        kind,
		PatientID:   metadataString(msg.Metadata, "patient_id"),
		CaseID:      metadataString(msg.Metadata, "case_id"),
		Priority:    priority,
		Subject:     subject,
		Detail:      msg.Content,
		NextTo:      nextTo,
		NextType:    nextType,
		Payload:     msg.Content,
		Meta:        forwardMeta,
		SLAAt:       sla,
		SubmittedBy: msg.From,
	})
	env.Logf("[human_review] pending id=%s kind=%s patient=%s priority=%s next=%s/%s",
		req.ID, kind, req.PatientID, priority, nextTo, nextType)
	return nil, nil
}

// ApplyDecision records a reviewer's decision and returns the resume messages
// the caller should publish on the bus. On approve / approve_with_modifications,
// the original payload (optionally replaced by `modification`) is sent to
// NextTo with NextType. On reject, no message is returned. ok=false if the id
// is unknown, already decided, or the decision string is invalid.
func (a *HumanReviewAgent) ApplyDecision(id, reviewerID, decision, reason, modification string) (medical.ReviewRequest, []agent.Message, bool) {
	r, ok := a.Queue.Decide(id, reviewerID, decision, reason, modification)
	if !ok {
		return r, nil, false
	}
	if r.Status != medical.ReviewApproved && r.Status != medical.ReviewModified {
		return r, nil, true
	}
	payload := r.Payload
	if r.Status == medical.ReviewModified && modification != "" {
		payload = modification
	}
	meta := copyMeta(r.Meta)
	meta["hitl_review_id"] = r.ID
	meta["hitl_reviewer_id"] = reviewerID
	meta["hitl_kind"] = "" // sentinel: cleared envelope, just an audit breadcrumb
	delete(meta, "hitl_kind")
	out := agent.NewMessage(a.ID(), r.NextTo, agent.RoleAgent, r.NextType, payload, meta)
	return r, []agent.Message{out}, true
}

// reviewOptions configures the HITL gate wrapped around a downstream message.
type reviewOptions struct {
	Kind       string
	NextTo     string
	NextType   string
	Priority   string
	Subject    string
	SLAMinutes float64
}

// reviewEnvelope adds HITL envelope keys to a copy of meta, so the upstream
// agent can address the message to "human_review" with type "review_request"
// while preserving the original target in Metadata.
func reviewEnvelope(meta map[string]any, opts reviewOptions) map[string]any {
	m := copyMeta(meta)
	m["hitl_kind"] = opts.Kind
	m["hitl_next_to"] = opts.NextTo
	m["hitl_next_type"] = opts.NextType
	if opts.Priority != "" {
		m["hitl_priority"] = opts.Priority
	}
	if opts.Subject != "" {
		m["hitl_subject"] = opts.Subject
	}
	if opts.SLAMinutes > 0 {
		m["hitl_sla_minutes"] = opts.SLAMinutes
	}
	return m
}
