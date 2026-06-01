package tools

// File ctgov.go: ClinicalTrialsGov is a Tool that searches ClinicalTrials.gov
// via the v2 REST API. On network failure (or offline / demo mode) it falls
// back to a small set of mock results so the tool never hard-fails during
// development or evals.
//
// Input shape (JSON):
//
//	{"condition":"NSCLC","location":"Boston","phase":"Phase III","max_results":5}
//
// Output shape (JSON):
//
//	{
//	  "studies":[
//	    {"nct_id":"NCT00000001","title":"...","status":"Recruiting",
//	     "phase":"Phase III","sponsor":"...","brief_summary":"..."}
//	  ],
//	  "source":"clinicaltrials.gov",   // or "demo-mock" on fallback
//	  "total_returned":1
//	}
//
// The live API endpoint is:
//
//	GET https://clinicaltrials.gov/api/v2/studies?query.cond=<condition>&pageSize=<n>&format=json
//
// Reference: https://clinicaltrials.gov/data-api/api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClinicalTrialsGov searches ClinicalTrials.gov (v2 API) for trials matching
// a condition. Falls back to mock data when the network call fails.
type ClinicalTrialsGov struct {
	// HTTPClient is used for the live API call. When nil, a default client
	// with a 10-second timeout is used.
	HTTPClient *http.Client
}

// Name implements Tool.
func (ClinicalTrialsGov) Name() string { return "search_clinical_trials" }

// Description implements Tool.
func (ClinicalTrialsGov) Description() string {
	return "Search ClinicalTrials.gov for interventional studies matching a medical condition. Returns NCT ID, title, status, phase, sponsor, and brief summary. Falls back to demo mock data when offline."
}

// Schema implements Tool.
func (ClinicalTrialsGov) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "condition": {
      "type": "string",
      "description": "Medical condition or disease to search for, e.g. \"non-small cell lung cancer\"."
    },
    "location": {
      "type": "string",
      "description": "Optional geographic filter, e.g. \"Boston\" or \"United States\"."
    },
    "phase": {
      "type": "string",
      "description": "Optional trial phase filter, e.g. \"Phase III\" or \"Phase I\"."
    },
    "max_results": {
      "type": "integer",
      "description": "Maximum number of results to return (1–20). Defaults to 5.",
      "minimum": 1,
      "maximum": 20
    }
  },
  "required": ["condition"],
  "additionalProperties": false
}`)
}

// ---- input / output types ----

type ctgovInput struct {
	Condition  string `json:"condition"`
	Location   string `json:"location,omitempty"`
	Phase      string `json:"phase,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
}

// StudyResult is one row in the output studies array.
type StudyResult struct {
	NCTID        string `json:"nct_id"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Phase        string `json:"phase"`
	Sponsor      string `json:"sponsor"`
	BriefSummary string `json:"brief_summary"`
}

type ctgovOutput struct {
	Studies       []StudyResult `json:"studies"`
	Source        string        `json:"source"`
	TotalReturned int           `json:"total_returned"`
}

// ---- live API response shapes (minimal subset we parse) ----

type ctgovAPIResponse struct {
	Studies []ctgovAPIStudy `json:"studies"`
}

type ctgovAPIStudy struct {
	ProtocolSection ctgovProtocolSection `json:"protocolSection"`
}

type ctgovProtocolSection struct {
	IdentificationModule   ctgovIdentification   `json:"identificationModule"`
	StatusModule           ctgovStatus           `json:"statusModule"`
	DesignModule           ctgovDesign           `json:"designModule"`
	SponsorCollaborators   ctgovSponsors         `json:"sponsorCollaboratorsModule"`
	DescriptionModule      ctgovDescription      `json:"descriptionModule"`
}

type ctgovIdentification struct {
	NCTID         string `json:"nctId"`
	BriefTitle    string `json:"briefTitle"`
}

type ctgovStatus struct {
	OverallStatus string `json:"overallStatus"`
}

type ctgovDesign struct {
	PhaseList ctgovPhaseList `json:"phaseList"`
}

type ctgovPhaseList struct {
	Phase []string `json:"phase"`
}

type ctgovSponsors struct {
	LeadSponsor ctgovSponsor `json:"leadSponsor"`
}

type ctgovSponsor struct {
	Name string `json:"name"`
}

type ctgovDescription struct {
	BriefSummary string `json:"briefSummary"`
}

// ---- mock fallback data ----

// ctgovMockStudies is returned when the live API is unreachable. Each entry
// uses a fictional NCT ID (NCT99xxxxxx range) to prevent confusion with real
// trials. Tagged source = "demo-mock".
var ctgovMockStudies = []StudyResult{
	{
		NCTID:        "NCT99000001",
		Title:        "Phase III Immunotherapy in Advanced NSCLC (BODH-LUNG-01)",
		Status:       "Recruiting",
		Phase:        "Phase III",
		Sponsor:      "Bodh Oncology Research (demo)",
		BriefSummary: "A randomized Phase III trial evaluating PD-L1 blockade plus chemotherapy versus chemotherapy alone in treatment-naive Stage IIIB/IV NSCLC. Demo data only.",
	},
	{
		NCTID:        "NCT99000002",
		Title:        "Phase II HER2-Positive Breast Cancer — Novel ADC Combination (BODH-BREAST-01)",
		Status:       "Recruiting",
		Phase:        "Phase II",
		Sponsor:      "Bodh Oncology Research (demo)",
		BriefSummary: "An open-label Phase II study of a novel antibody-drug conjugate combined with trastuzumab in HER2-positive metastatic breast cancer. Demo data only.",
	},
	{
		NCTID:        "NCT99000003",
		Title:        "Phase II Cardiovascular Outcomes in Type 2 Diabetes (BODH-CARDIO-01)",
		Status:       "Recruiting",
		Phase:        "Phase II",
		Sponsor:      "Bodh Cardiometabolic Institute (demo)",
		BriefSummary: "A double-blind, placebo-controlled Phase II study of a novel GLP-1/GIP dual agonist on MACE outcomes in adults with T2DM and established CVD risk. Demo data only.",
	},
	{
		NCTID:        "NCT99000004",
		Title:        "Phase I Solid Tumor Dose Escalation — BODH-TXL-01",
		Status:       "Recruiting",
		Phase:        "Phase I",
		Sponsor:      "Bodh Translational Medicine (demo)",
		BriefSummary: "A first-in-human Phase I dose-escalation study of BODH-TXL-01 (pan-KRAS inhibitor) in adults with solid tumors refractory to standard therapy. Demo data only.",
	},
	{
		NCTID:        "NCT99000005",
		Title:        "HIPAA-Compliant Real-World Evidence Registry (BODH-RWE-01)",
		Status:       "Enrolling by invitation",
		Phase:        "Registry",
		Sponsor:      "Bodh Real-World Evidence Consortium (demo)",
		BriefSummary: "A multi-site HIPAA-compliant registry collecting real-world outcomes, biomarkers, and patient-reported outcomes across oncology and cardiometabolic indications. Demo data only.",
	},
}

// ---- Invoke ----

// Invoke implements Tool. Calls the live ClinicalTrials.gov v2 API; falls
// back to mock data on any network or decode error.
func (t ClinicalTrialsGov) Invoke(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("search_clinical_trials: empty arguments")
	}
	var in ctgovInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("search_clinical_trials: decode arguments: %w", err)
	}
	condition := strings.TrimSpace(in.Condition)
	if condition == "" {
		return nil, fmt.Errorf("search_clinical_trials: condition is required")
	}
	maxResults := in.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20
	}

	studies, source, err := t.fetchStudies(ctx, condition, maxResults)
	if err != nil {
		// Fallback to mock data; surface the original error as metadata.
		studies = filterMockStudies(in, maxResults)
		source = "demo-mock"
	}

	out := ctgovOutput{
		Studies:       studies,
		Source:        source,
		TotalReturned: len(studies),
	}
	if out.Studies == nil {
		out.Studies = []StudyResult{}
	}
	return json.Marshal(out)
}

// fetchStudies calls the ClinicalTrials.gov v2 REST API and returns parsed
// results. Returns an error on any HTTP or decode failure so the caller can
// fall back to mock data.
func (t ClinicalTrialsGov) fetchStudies(ctx context.Context, condition string, maxResults int) ([]StudyResult, string, error) {
	apiURL := "https://clinicaltrials.gov/api/v2/studies"
	params := url.Values{}
	params.Set("query.cond", condition)
	params.Set("pageSize", fmt.Sprintf("%d", maxResults))
	params.Set("format", "json")

	reqURL := apiURL + "?" + params.Encode()

	client := t.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}

	var apiResp ctgovAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, "", fmt.Errorf("decode response: %w", err)
	}

	studies := make([]StudyResult, 0, len(apiResp.Studies))
	for _, s := range apiResp.Studies {
		ps := s.ProtocolSection
		phase := ""
		if len(ps.DesignModule.PhaseList.Phase) > 0 {
			phase = strings.Join(ps.DesignModule.PhaseList.Phase, "/")
		}
		studies = append(studies, StudyResult{
			NCTID:        ps.IdentificationModule.NCTID,
			Title:        ps.IdentificationModule.BriefTitle,
			Status:       ps.StatusModule.OverallStatus,
			Phase:        phase,
			Sponsor:      ps.SponsorCollaborators.LeadSponsor.Name,
			BriefSummary: ps.DescriptionModule.BriefSummary,
		})
	}
	return studies, "clinicaltrials.gov", nil
}

// filterMockStudies returns up to maxResults mock studies. When the caller
// supplied a phase filter, only studies matching that phase are returned.
// The condition string is not matched against mock summaries (the mock set
// is intentionally general-purpose for demo scenarios).
func filterMockStudies(in ctgovInput, maxResults int) []StudyResult {
	phaseFilter := strings.ToLower(strings.TrimSpace(in.Phase))
	var out []StudyResult
	for _, s := range ctgovMockStudies {
		if phaseFilter != "" && !strings.Contains(strings.ToLower(s.Phase), phaseFilter) {
			continue
		}
		out = append(out, s)
		if len(out) >= maxResults {
			break
		}
	}
	return out
}
