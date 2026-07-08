package medical

import (
	"encoding/json"
	"time"
)

// ReviewStatus is the lifecycle of a human-in-the-loop review.
type ReviewStatus string

const (
	ReviewPending  ReviewStatus = "pending"
	ReviewApproved ReviewStatus = "approved"
	ReviewModified ReviewStatus = "approved_with_modifications"
	ReviewRejected ReviewStatus = "rejected"
	ReviewExpired  ReviewStatus = "expired"
)

// ReviewRequest is one clinical action awaiting human review. Upstream agents
// emit a review_request message that the human_review agent records here; a
// reviewer (clinician or nurse) decides via the HTTP API or programmatically,
// at which point the original action resumes onto the bus.
type ReviewRequest struct {
	ID           string         `json:"id"`
	Kind         string         `json:"kind"` // diagnosis_review, care_plan_review, ...
	Status       ReviewStatus   `json:"status"`
	PatientID    string         `json:"patient_id,omitempty"`
	CaseID       string         `json:"case_id,omitempty"`
	Priority     string         `json:"priority"` // low/normal/high/urgent
	Subject      string         `json:"subject"`
	Detail       string         `json:"detail,omitempty"`
	NextTo       string         `json:"next_to"`   // agent to forward to on approval
	NextType     string         `json:"next_type"` // message type to use on resume
	Payload      string         `json:"payload"`   // body to forward
	Meta         map[string]any `json:"meta,omitempty"`
	SLAAt        time.Time      `json:"sla_at,omitempty"`
	SubmittedAt  time.Time      `json:"submitted_at"`
	SubmittedBy  string         `json:"submitted_by,omitempty"`
	ReviewerID   string         `json:"reviewer_id,omitempty"`
	DecidedAt    time.Time      `json:"decided_at,omitempty"`
	Reason       string         `json:"reason,omitempty"`
	Modification string         `json:"modification,omitempty"`
}

// EncodeReview / DecodeReview move ReviewRequest as message Content.
func EncodeReview(r ReviewRequest) string {
	b, _ := json.Marshal(r)
	return string(b)
}

func DecodeReview(content string) (ReviewRequest, error) {
	var r ReviewRequest
	if err := json.Unmarshal([]byte(content), &r); err != nil {
		return ReviewRequest{}, err
	}
	return r, nil
}
