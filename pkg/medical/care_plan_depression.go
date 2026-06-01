package medical

import "time"

// BuildDepressionCarePlan returns a PHQ-9-score-stratified care plan for a patient
// with a depression or major depressive disorder diagnosis.
//
// PHQ-9 score bands (educational demo — not a clinical guideline):
//
//	0-4:  minimal    → monitoring only, recheck at 12 weeks
//	5-9:  mild       → lifestyle counseling, PHQ-9 recheck in 4 weeks
//	10-14: moderate  → consider SSRI (sertraline 50 mg) + therapy referral
//	15-19: moderately severe → SSRI + therapy + close follow-up (2 weeks)
//	20-27: severe    → urgent psychiatry referral + safety plan
func BuildDepressionCarePlan(caseID, patientID string, phq9Score int, t time.Time) CarePlan {
	plan := CarePlan{
		CaseID:    caseID,
		PatientID: patientID,
		Condition: "depression",
		CreatedAt: t.UTC(),
		Goals: []GoalEntry{
			{Metric: "phq9_score", Target: 5, Unit: "score", Window: "12w"},
		},
		Monitoring: []MonitoringEntry{
			{Metric: "phq9_score", Cadence: "monthly", Source: "self_report"},
		},
	}

	switch {
	case phq9Score <= 4:
		// Minimal — watch and wait.
		plan.Education = []string{
			"Sleep hygiene education",
			"Regular physical activity",
			"PHQ-9 recheck at 12 weeks",
		}
		plan.NextReview = t.AddDate(0, 3, 0) // 12 weeks

	case phq9Score <= 9:
		// Mild — lifestyle intervention, earlier recheck.
		plan.Education = []string{
			"Behavioral activation counseling",
			"Sleep hygiene education",
			"Regular physical activity (≥150 min/week)",
			"PHQ-9 recheck in 4 weeks",
		}
		plan.NextReview = t.AddDate(0, 0, 28) // 4 weeks

	case phq9Score <= 14:
		// Moderate — SSRI initiation + therapy referral.
		plan.Medications = []MedicationEntry{
			{Name: "Sertraline", Dose: "50 mg", Frequency: "daily", Class: "SSRI"},
		}
		plan.Education = []string{
			"Cognitive behavioral therapy (CBT) referral",
			"SSRI side-effect counseling",
			"PHQ-9 recheck in 4 weeks",
		}
		plan.NextReview = t.AddDate(0, 0, 28) // 4 weeks

	case phq9Score <= 19:
		// Moderately severe — SSRI + therapy + close follow-up.
		plan.Medications = []MedicationEntry{
			{Name: "Sertraline", Dose: "50 mg", Frequency: "daily", Class: "SSRI"},
		}
		plan.Education = []string{
			"Cognitive behavioral therapy (CBT) referral",
			"SSRI side-effect counseling",
			"Safety planning discussion",
			"PHQ-9 recheck in 2 weeks",
		}
		plan.NextReview = t.AddDate(0, 0, 14) // 2 weeks

	default: // score >= 20 — Severe
		// Urgent psychiatry referral + safety plan.
		plan.Medications = []MedicationEntry{
			{Name: "Sertraline", Dose: "50 mg", Frequency: "daily", Class: "SSRI"},
		}
		plan.Education = []string{
			"Urgent psychiatry referral",
			"Safety plan completed",
			"Crisis line: 988 Suicide & Crisis Lifeline",
			"PHQ-9 recheck in 1 week",
		}
		plan.NextReview = t.AddDate(0, 0, 7) // 1 week
	}

	return plan
}
