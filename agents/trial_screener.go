package agents

// File trial_screener.go: ClinicalTrialScreenerAgent — screens a patient
// against an in-process mock trial database and returns eligible trials
// ranked by match score.
//
// The agent follows the same HandleMessage → []agent.Message pattern used by
// every other agent in this package. Input is a JSON-encoded
// TrialScreenRequest; output is a JSON-encoded TrialScreenResult forwarded
// to "diagnostic_supervisor".
//
// The five fictional trials (BODHYANGA-001 … BODHYANGA-005) are illustrative only —
// they exist solely to demonstrate the screening pipeline. No real trials,
// sponsors, or eligibility criteria are represented.
//
// Match score semantics: fraction of criteria that pass (0.0–1.0). Only
// trials with match_score >= 0.5 appear in the eligible_trials list. Trials
// with a perfect score (1.0) are returned first; ties are broken by trial ID.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
)

// -------- input / output types --------

// TrialScreenRequest is the Content of a "trial_screen_request" message.
type TrialScreenRequest struct {
	PatientID       string   `json:"patient_id"`
	CaseID          string   `json:"case_id"`
	Diagnosis       string   `json:"diagnosis"`
	Age             int      `json:"age"`
	ECOGScore       int      `json:"ecog_score"`
	PriorTreatments []string `json:"prior_treatments"`
	Exclusions      []string `json:"exclusions"`
}

// TrialMatch is one row inside TrialScreenResult.EligibleTrials.
type TrialMatch struct {
	TrialID        string   `json:"trial_id"`
	Title          string   `json:"title"`
	Phase          string   `json:"phase"`
	Sponsor        string   `json:"sponsor"`
	CriteriaMet    []string `json:"criteria_met"`
	CriteriaFailed []string `json:"criteria_failed"`
	MatchScore     float64  `json:"match_score"`
}

// TrialScreenResult is the Content of a "trial_screen_result" message.
type TrialScreenResult struct {
	EligibleTrials []TrialMatch `json:"eligible_trials"`
	TotalScreened  int          `json:"total_screened"`
}

// -------- mock trial database --------

// trialSpec is the internal representation of one trial in the mock DB.
// Each trial carries a slice of criterion functions; the screener calls every
// function and counts how many return true.
type trialSpec struct {
	trialID  string
	title    string
	phase    string
	sponsor  string
	criteria []trialCriterion
}

// trialCriterion is one eligibility check. label is the human-readable
// description shown in criteria_met / criteria_failed; check returns true
// when the patient satisfies the criterion.
type trialCriterion struct {
	label string
	check func(r TrialScreenRequest) bool
}

// mockTrials is the in-memory trial database used for offline / demo mode.
// All five entries are fictional. Criteria are intentionally simple so the
// matching logic is auditable without clinical domain knowledge.
var mockTrials = []trialSpec{
	{
		trialID: "BODHYANGA-001",
		title:   "Phase III NSCLC immunotherapy",
		phase:   "Phase III",
		sponsor: "Bodhyanga Oncology Research",
		criteria: []trialCriterion{
			{
				label: "age >= 18",
				check: func(r TrialScreenRequest) bool { return r.Age >= 18 },
			},
			{
				label: "NSCLC diagnosis",
				check: func(r TrialScreenRequest) bool {
					d := strings.ToLower(r.Diagnosis)
					return strings.Contains(d, "nsclc") ||
						strings.Contains(d, "non-small cell") ||
						strings.Contains(d, "non small cell")
				},
			},
			{
				label: "ECOG 0-2",
				check: func(r TrialScreenRequest) bool { return r.ECOGScore >= 0 && r.ECOGScore <= 2 },
			},
			{
				label: "no prior checkpoint inhibitors",
				check: func(r TrialScreenRequest) bool {
					for _, t := range r.PriorTreatments {
						tl := strings.ToLower(t)
						if strings.Contains(tl, "checkpoint") ||
							strings.Contains(tl, "pd-1") ||
							strings.Contains(tl, "pd-l1") ||
							strings.Contains(tl, "pembrolizumab") ||
							strings.Contains(tl, "nivolumab") ||
							strings.Contains(tl, "atezolizumab") {
							return false
						}
					}
					return true
				},
			},
		},
	},
	{
		trialID: "BODHYANGA-002",
		title:   "Phase II breast cancer HER2+",
		phase:   "Phase II",
		sponsor: "Bodhyanga Oncology Research",
		criteria: []trialCriterion{
			{
				label: "age >= 18",
				check: func(r TrialScreenRequest) bool { return r.Age >= 18 },
			},
			{
				label: "breast cancer diagnosis",
				check: func(r TrialScreenRequest) bool {
					return strings.Contains(strings.ToLower(r.Diagnosis), "breast")
				},
			},
			{
				label: "HER2+ in diagnosis",
				check: func(r TrialScreenRequest) bool {
					d := strings.ToLower(r.Diagnosis)
					return strings.Contains(d, "her2+") ||
						strings.Contains(d, "her2 positive") ||
						strings.Contains(d, "her-2+")
				},
			},
			{
				label: "ECOG 0-1",
				check: func(r TrialScreenRequest) bool { return r.ECOGScore >= 0 && r.ECOGScore <= 1 },
			},
		},
	},
	{
		trialID: "BODHYANGA-003",
		title:   "Phase II DM2 cardiovascular outcomes",
		phase:   "Phase II",
		sponsor: "Bodhyanga Cardiometabolic Institute",
		criteria: []trialCriterion{
			{
				label: "age >= 40",
				check: func(r TrialScreenRequest) bool { return r.Age >= 40 },
			},
			{
				label: "diabetes (type 2) diagnosis",
				check: func(r TrialScreenRequest) bool {
					d := strings.ToLower(r.Diagnosis)
					return strings.Contains(d, "diabetes") ||
						strings.Contains(d, "dm2") ||
						strings.Contains(d, "type 2 dm") ||
						strings.Contains(d, "t2dm")
				},
			},
			{
				label: "no prior MACE (MI or stroke in exclusions)",
				check: func(r TrialScreenRequest) bool {
					for _, ex := range r.Exclusions {
						el := strings.ToLower(ex)
						if strings.Contains(el, "mi") ||
							strings.Contains(el, "myocardial infarction") ||
							strings.Contains(el, "stroke") ||
							strings.Contains(el, "mace") {
							return false
						}
					}
					return true
				},
			},
		},
	},
	{
		trialID: "BODHYANGA-004",
		title:   "Phase I solid tumor dose escalation",
		phase:   "Phase I",
		sponsor: "Bodhyanga Translational Medicine",
		criteria: []trialCriterion{
			{
				label: "solid tumor diagnosis",
				check: func(r TrialScreenRequest) bool {
					d := strings.ToLower(r.Diagnosis)
					solidKeywords := []string{
						"tumor", "carcinoma", "cancer", "adenocarcinoma",
						"sarcoma", "melanoma", "lymphoma", "nsclc",
						"breast", "colon", "lung", "prostate",
					}
					for _, kw := range solidKeywords {
						if strings.Contains(d, kw) {
							return true
						}
					}
					return false
				},
			},
			{
				label: "ECOG 0-2",
				check: func(r TrialScreenRequest) bool { return r.ECOGScore >= 0 && r.ECOGScore <= 2 },
			},
			// Prior chemo allowed — always passes; included for transparency.
			{
				label: "prior chemotherapy allowed",
				check: func(r TrialScreenRequest) bool { return true },
			},
		},
	},
	{
		trialID: "BODHYANGA-005",
		title:   "Registry: HIPAA-compliant real-world evidence",
		phase:   "Registry",
		sponsor: "Bodhyanga Real-World Evidence Consortium",
		criteria: []trialCriterion{
			{
				label: "any diagnosis",
				check: func(r TrialScreenRequest) bool { return strings.TrimSpace(r.Diagnosis) != "" },
			},
			{
				label: "age >= 18",
				check: func(r TrialScreenRequest) bool { return r.Age >= 18 },
			},
		},
	},
}

// -------- agent --------

// ClinicalTrialScreenerAgent screens a patient against the mock trial
// database and emits a "trial_screen_result" message to diagnostic_supervisor.
type ClinicalTrialScreenerAgent struct{}

// ID implements agent.Agent.
func (a *ClinicalTrialScreenerAgent) ID() string { return "trial_screener" }

// Name implements agent.Agent.
func (a *ClinicalTrialScreenerAgent) Name() string {
	return "Clinical Trial Eligibility Screener"
}

// Capabilities implements agent.Agent.
func (a *ClinicalTrialScreenerAgent) Capabilities() []string {
	return []string{"trial_eligibility", "research_screening"}
}

// HandleMessage implements agent.Agent. It expects msg.Type ==
// "trial_screen_request" and msg.Content to be a JSON-encoded
// TrialScreenRequest. It returns a single message of type
// "trial_screen_result" addressed to "diagnostic_supervisor".
func (a *ClinicalTrialScreenerAgent) HandleMessage(
	ctx context.Context,
	msg agent.Message,
	env agent.Environment,
) ([]agent.Message, error) {
	var req TrialScreenRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		return nil, fmt.Errorf("trial_screener: decode request: %w", err)
	}

	result := screenTrials(req)

	env.Logf("[trial_screener] patient=%s screened=%d eligible=%d",
		req.PatientID, result.TotalScreened, len(result.EligibleTrials))

	content, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("trial_screener: encode result: %w", err)
	}

	out := agent.NewMessage(
		a.ID(),
		"diagnostic_supervisor",
		agent.RoleAgent,
		"trial_screen_result",
		string(content),
		copyMeta(msg.Metadata),
	)
	return []agent.Message{out}, nil
}

// screenTrials runs every mock trial through its eligibility criteria and
// returns the matches with match_score >= 0.5, sorted descending by score.
func screenTrials(req TrialScreenRequest) TrialScreenResult {
	var matches []TrialMatch

	for _, spec := range mockTrials {
		var met, failed []string
		for _, c := range spec.criteria {
			if c.check(req) {
				met = append(met, c.label)
			} else {
				failed = append(failed, c.label)
			}
		}

		total := len(spec.criteria)
		score := 0.0
		if total > 0 {
			score = float64(len(met)) / float64(total)
		}

		if score >= 0.5 {
			if met == nil {
				met = []string{}
			}
			if failed == nil {
				failed = []string{}
			}
			matches = append(matches, TrialMatch{
				TrialID:        spec.trialID,
				Title:          spec.title,
				Phase:          spec.phase,
				Sponsor:        spec.sponsor,
				CriteriaMet:    met,
				CriteriaFailed: failed,
				MatchScore:     score,
			})
		}
	}

	// Sort descending by match score; break ties by trial ID (ascending).
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].MatchScore != matches[j].MatchScore {
			return matches[i].MatchScore > matches[j].MatchScore
		}
		return matches[i].TrialID < matches[j].TrialID
	})

	result := TrialScreenResult{
		TotalScreened:  len(mockTrials),
		EligibleTrials: matches,
	}
	if result.EligibleTrials == nil {
		result.EligibleTrials = []TrialMatch{}
	}
	return result
}
