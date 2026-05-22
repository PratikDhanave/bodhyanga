package medical

import (
	"fmt"
	"time"
)

// CareGap is a single open quality / preventive-care issue.
type CareGap struct {
	PatientID string `json:"patient_id"`
	Rule      string `json:"rule"`
	Severity  string `json:"severity"` // info, warn, urgent
	Detail    string `json:"detail"`
}

// PanelReport is the aggregate output of the panel manager.
type PanelReport struct {
	GeneratedAt   time.Time `json:"generated_at"`
	PanelSize     int       `json:"panel_size"`
	Gaps          []CareGap `json:"gaps,omitempty"`
	ConditionRoll map[string]int `json:"condition_roll,omitempty"`
}

// PanelRule evaluates one patient and returns zero or more gaps.
type PanelRule func(p PatientRecord, now time.Time) []CareGap

// DefaultPanelRules returns the demo rule set: A1c overdue, BP uncontrolled,
// no recent visit, RPM weight trend for CHF, post-discharge follow-up window.
func DefaultPanelRules() []PanelRule {
	return []PanelRule{
		a1cOverdueRule,
		bpUncontrolledRule,
		annualVisitRule,
		chfWeightRule,
		postDischargeFollowUpRule,
	}
}

func a1cOverdueRule(p PatientRecord, now time.Time) []CareGap {
	if !hasCondition(p, "dm") {
		return nil
	}
	if p.LastA1cDate.IsZero() || now.Sub(p.LastA1cDate) > 180*24*time.Hour {
		return []CareGap{{
			PatientID: p.PatientID, Rule: "a1c_overdue", Severity: "warn",
			Detail: "Diabetic patient with no A1c in last 180 days",
		}}
	}
	if p.LastA1c >= 9.0 {
		return []CareGap{{
			PatientID: p.PatientID, Rule: "a1c_uncontrolled", Severity: "urgent",
			Detail: fmt.Sprintf("A1c %.1f%% (target <7%%)", p.LastA1c),
		}}
	}
	return nil
}

func bpUncontrolledRule(p PatientRecord, _ time.Time) []CareGap {
	if !hasCondition(p, "htn") {
		return nil
	}
	if p.LastSystolicBP >= 160 {
		return []CareGap{{
			PatientID: p.PatientID, Rule: "bp_uncontrolled", Severity: "urgent",
			Detail: fmt.Sprintf("SBP %.0f mmHg (target <130)", p.LastSystolicBP),
		}}
	}
	if p.LastSystolicBP >= 140 {
		return []CareGap{{
			PatientID: p.PatientID, Rule: "bp_elevated", Severity: "warn",
			Detail: fmt.Sprintf("SBP %.0f mmHg", p.LastSystolicBP),
		}}
	}
	return nil
}

func annualVisitRule(p PatientRecord, now time.Time) []CareGap {
	if p.LastVisit.IsZero() || now.Sub(p.LastVisit) > 365*24*time.Hour {
		return []CareGap{{
			PatientID: p.PatientID, Rule: "annual_visit_due", Severity: "info",
			Detail: "No encounter in last 12 months",
		}}
	}
	return nil
}

func chfWeightRule(p PatientRecord, _ time.Time) []CareGap {
	if !hasCondition(p, "chf") {
		return nil
	}
	if p.LastWeightLb >= 190 {
		return []CareGap{{
			PatientID: p.PatientID, Rule: "chf_weight_alert", Severity: "urgent",
			Detail: fmt.Sprintf("Weight %.0f lb above dry-weight threshold", p.LastWeightLb),
		}}
	}
	return nil
}

func postDischargeFollowUpRule(p PatientRecord, now time.Time) []CareGap {
	if p.DischargedAt.IsZero() {
		return nil
	}
	days := int(now.Sub(p.DischargedAt).Hours() / 24)
	if days >= 7 && days <= 30 && (p.LastVisit.IsZero() || p.LastVisit.Before(p.DischargedAt)) {
		return []CareGap{{
			PatientID: p.PatientID, Rule: "tcm_followup_open", Severity: "urgent",
			Detail: fmt.Sprintf("Discharged %d days ago, no post-discharge visit", days),
		}}
	}
	return nil
}

// BuildPanelReport runs every rule over every patient and aggregates.
func BuildPanelReport(patients []PatientRecord, rules []PanelRule, now time.Time) PanelReport {
	rep := PanelReport{
		GeneratedAt:   now.UTC(),
		PanelSize:     len(patients),
		ConditionRoll: map[string]int{},
	}
	for _, p := range patients {
		for _, c := range p.Conditions {
			rep.ConditionRoll[c]++
		}
		for _, rule := range rules {
			rep.Gaps = append(rep.Gaps, rule(p, now)...)
		}
	}
	return rep
}

func hasCondition(p PatientRecord, code string) bool {
	for _, c := range p.Conditions {
		if c == code {
			return true
		}
	}
	return false
}
