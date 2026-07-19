package agents

// ReviewRequest is what the physician sees when a case pauses for review.
// It carries the hashed case token and the diagnosis under review — enough
// to decide, keyed PHI-safe (the full case stays in the workflow payload).
type ReviewRequest struct {
	CaseToken  string
	Diagnosis  string
	Confidence float64
	Forced     bool
	Rounds     int
	CostUSD    float64 // total test cost so the reviewer sees the Pareto (specs/006)
}

// ReviewDecision is the physician's explicit verdict. ReviewerID is
// mandatory — an anonymous approval never lands (spec FR-007/FR-009).
type ReviewDecision struct {
	Approved   bool
	ReviewerID string
	Note       string
}

// CaseRejected is the terminal output when a physician rejects the
// diagnosis: the case ends without a care plan.
type CaseRejected struct {
	CaseID     string
	Diagnosis  string
	ReviewerID string
	Note       string
}
