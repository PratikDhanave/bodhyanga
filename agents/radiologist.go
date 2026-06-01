package agents

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// RadiologistAgent interprets imaging reports using rule-based critical finding
// detection. Text-based today; DICOM SR integration planned for a future release.
//
// Input message Type:  radiology_request
//
//	Content: {"patient_id","case_id","modality":"CXR|CT|MRI|US","report_text":string,"body_part":string}
//
// Output message Type: radiology_report
//
//	Content: {"findings":[]string,"impression":string,"critical_finding":bool,"follow_up_needed":bool,"urgency":"routine|urgent|emergent"}
type RadiologistAgent struct{}

func (a *RadiologistAgent) ID() string   { return "radiologist" }
func (a *RadiologistAgent) Name() string { return "Radiology Specialist" }
func (a *RadiologistAgent) Capabilities() []string {
	return []string{"imaging_interpretation", "radiology_report"}
}

// radiologyRequest is the inbound payload from a radiology_request message.
type radiologyRequest struct {
	PatientID  string `json:"patient_id"`
	CaseID     string `json:"case_id,omitempty"`
	Modality   string `json:"modality"`    // CXR | CT | MRI | US
	ReportText string `json:"report_text"` // free-text radiology report
	BodyPart   string `json:"body_part"`
}

// radiologyReport is the outbound payload emitted as a radiology_report message.
type radiologyReport struct {
	Findings        []string `json:"findings"`
	Impression      string   `json:"impression"`
	CriticalFinding bool     `json:"critical_finding"`
	FollowUpNeeded  bool     `json:"follow_up_needed"`
	Urgency         string   `json:"urgency"` // routine | urgent | emergent
}

// HandleMessage decodes the inbound radiology_request, applies rule-based
// critical finding detection, and emits a single radiology_report message
// back to the requesting agent (msg.From).
func (a *RadiologistAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req radiologyRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[radiologist] invalid request: %v", err)
		return nil, err
	}

	report := interpretRadiologyReport(req)
	env.Logf("[radiologist] patient=%s modality=%s urgency=%s critical=%v",
		req.PatientID, req.Modality, report.Urgency, report.CriticalFinding)

	payload, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}

	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	if req.CaseID != "" {
		meta["case_id"] = req.CaseID
	}

	out := agent.NewMessage(a.ID(), msg.From, agent.RoleAgent, "radiology_report", string(payload), meta)
	return []agent.Message{out}, nil
}

// interpretRadiologyReport applies rule-based critical finding detection to
// produce a radiologyReport. Rules are keyed on (modality, body_part) and
// matched against lower-cased report_text. Critical findings always force
// urgency=emergent and critical_finding=true.
//
// Detection hierarchy (evaluated in descending urgency order so that a report
// containing both "pneumothorax" and "infiltrate" correctly surfaces as
// emergent, not routine):
//
//	CXR:
//	  pneumothorax           → emergent
//	  pleural effusion large → urgent
//	  infiltrate             → routine
//
//	CT chest:
//	  pulmonary embolism     → emergent
//	  aortic dissection      → emergent
//	  pneumonia              → urgent
//
//	CT head:
//	  hemorrhage / bleed     → emergent
//	  stroke / infarct       → emergent
//	  mass                   → urgent
//
//	Abdomen (any modality):
//	  free air               → emergent
//	  obstruction            → urgent
//	  appendicitis           → urgent
func interpretRadiologyReport(req radiologyRequest) radiologyReport {
	text := strings.ToLower(req.ReportText)
	modality := strings.ToUpper(req.Modality)
	bodyPart := strings.ToLower(req.BodyPart)

	findings := extractRadiologyFindings(text, modality, bodyPart)
	urgency, critical := classifyRadiologyUrgency(findings)
	impression := buildRadiologyImpression(findings, urgency)

	return radiologyReport{
		Findings:        findings,
		Impression:      impression,
		CriticalFinding: critical,
		FollowUpNeeded:  urgency == "urgent" || urgency == "emergent",
		Urgency:         urgency,
	}
}

// radiologyFindingRule maps a keyword (matched against lower-cased report
// text) to a human-readable finding label and urgency tier.
type radiologyFindingRule struct {
	keyword string
	label   string // human-readable finding label for the report
	urgency string // routine | urgent | emergent
}

// radiologyRulesFor returns the ordered list of finding rules for a given
// modality and body part combination. Rules should be listed from highest to
// lowest urgency within each group so the first match in a scan loop naturally
// surfaces the most severe finding.
func radiologyRulesFor(modality, bodyPart string) []radiologyFindingRule {
	switch {
	case modality == "CXR":
		return []radiologyFindingRule{
			{"pneumothorax", "Pneumothorax", "emergent"},
			{"pleural effusion large", "Large pleural effusion", "urgent"},
			{"infiltrate", "Pulmonary infiltrate", "routine"},
		}
	case modality == "CT" && strings.Contains(bodyPart, "chest"):
		return []radiologyFindingRule{
			{"pulmonary embolism", "Pulmonary embolism", "emergent"},
			{"aortic dissection", "Aortic dissection", "emergent"},
			{"pneumonia", "Pneumonia", "urgent"},
		}
	case modality == "CT" && (strings.Contains(bodyPart, "head") || strings.Contains(bodyPart, "brain")):
		return []radiologyFindingRule{
			{"hemorrhage", "Intracranial hemorrhage", "emergent"},
			{"bleed", "Intracranial bleed", "emergent"},
			{"stroke", "Stroke", "emergent"},
			{"infarct", "Cerebral infarction", "emergent"},
			{"mass", "Intracranial mass", "urgent"},
		}
	case strings.Contains(bodyPart, "abdomen") || strings.Contains(bodyPart, "abdominal") || strings.Contains(bodyPart, "pelvis"):
		return []radiologyFindingRule{
			{"free air", "Free intraperitoneal air", "emergent"},
			{"obstruction", "Bowel obstruction", "urgent"},
			{"appendicitis", "Appendicitis", "urgent"},
		}
	default:
		return nil
	}
}

// extractRadiologyFindings matches all applicable rules against the
// lower-cased report text and returns the human-readable finding labels for
// every rule that fires. Duplicate labels are suppressed via a seen set.
// Returns ["No acute findings identified"] when no rule fires.
func extractRadiologyFindings(text, modality, bodyPart string) []string {
	var found []string
	seen := map[string]bool{}
	for _, r := range radiologyRulesFor(modality, bodyPart) {
		if strings.Contains(text, r.keyword) && !seen[r.label] {
			found = append(found, r.label)
			seen[r.label] = true
		}
	}
	if len(found) == 0 {
		return []string{"No acute findings identified"}
	}
	return found
}

// urgencyTierWeight maps urgency tiers to a numeric weight for comparison.
var urgencyTierWeight = map[string]int{
	"routine":  1,
	"urgent":   2,
	"emergent": 3,
}

// classifyRadiologyUrgency determines the highest urgency tier present across
// all findings and returns whether any finding is critical (emergent).
func classifyRadiologyUrgency(findings []string) (urgency string, critical bool) {
	idx := buildRadiologyLabelUrgencyIndex()
	best := "routine"
	for _, f := range findings {
		if u, ok := idx[f]; ok {
			if urgencyTierWeight[u] > urgencyTierWeight[best] {
				best = u
			}
		}
	}
	return best, best == "emergent"
}

// buildRadiologyLabelUrgencyIndex constructs a map from finding label to
// urgency tier by iterating over all known rule groups. This keeps
// classifyRadiologyUrgency consistent with radiologyRulesFor without
// duplicating the rule table.
func buildRadiologyLabelUrgencyIndex() map[string]string {
	idx := map[string]string{}
	groups := []struct {
		modality string
		bodyPart string
	}{
		{"CXR", ""},
		{"CT", "chest"},
		{"CT", "head"},
		{"CT", "abdomen"},
	}
	for _, g := range groups {
		for _, r := range radiologyRulesFor(g.modality, g.bodyPart) {
			idx[r.label] = r.urgency
		}
	}
	return idx
}

// buildRadiologyImpression assembles a concise impression string from detected
// findings and the computed urgency tier, mirroring the Impression section of
// a standard radiology report.
func buildRadiologyImpression(findings []string, urgency string) string {
	if len(findings) == 1 && findings[0] == "No acute findings identified" {
		return "No acute radiologic findings. Routine follow-up as clinically indicated."
	}
	prefix := map[string]string{
		"emergent": "EMERGENT: ",
		"urgent":   "URGENT: ",
		"routine":  "",
	}[urgency]
	return prefix + strings.Join(findings, "; ") + "."
}
