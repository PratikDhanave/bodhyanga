package medical

import (
	"fmt"
	"time"
)

// BuildPediatricWellChildCarePlan returns a deterministic well-child care plan
// based on age in months and weight in kilograms.
//
// The plan follows the ACIP immunization schedule and AAP developmental
// surveillance milestones. Weight-based dosing notes for antipyretics are
// included in the Education field.
//
// This is an educational/demonstration implementation — it is NOT a substitute
// for clinical judgment or current AAP/ACIP guidelines.
func BuildPediatricWellChildCarePlan(caseID, patientID string, ageMonths int, weightKg float64, t time.Time) CarePlan {
	plan := CarePlan{
		CaseID:    caseID,
		PatientID: patientID,
		Condition: "well_child",
		CreatedAt: t.UTC(),
	}

	// Standard growth and developmental goals for all ages.
	plan.Goals = []GoalEntry{
		{Metric: "weight_on_growth_curve", Target: 1, Unit: "percentile_band", Window: "visit"},
		{Metric: "height_on_growth_curve", Target: 1, Unit: "percentile_band", Window: "visit"},
		{Metric: "developmental_milestones_met", Target: 1, Unit: "boolean", Window: "visit"},
		{Metric: "vaccines_current", Target: 1, Unit: "boolean", Window: "visit"},
	}

	// Standard monitoring for all well-child visits.
	plan.Monitoring = []MonitoringEntry{
		{Metric: "weight_kg", Cadence: "visit", Source: "self_report"},
		{Metric: "height_cm", Cadence: "visit", Source: "self_report"},
		{Metric: "bmi_percentile", Cadence: "visit", Source: "self_report"},
		{Metric: "blood_pressure", Cadence: "visit", Source: "self_report"},
	}

	// Weight-based antipyretic dosing note.
	acetoMinDose := weightKg * 10
	acetoMaxDose := weightKg * 15
	ibuprofenMinDose := weightKg * 5
	ibuprofenMaxDose := weightKg * 10

	dosingNote := fmt.Sprintf(
		"Acetaminophen: %.0f-%.0f mg every 4-6 hours PRN fever (10-15 mg/kg, weight %.1f kg)",
		acetoMinDose, acetoMaxDose, weightKg,
	)

	plan.Education = []string{
		dosingNote,
		fmt.Sprintf(
			"Ibuprofen (>6 months): %.0f-%.0f mg every 6-8 hours PRN fever (5-10 mg/kg, weight %.1f kg)",
			ibuprofenMinDose, ibuprofenMaxDose, weightKg,
		),
		"Safe sleep practices: back-to-sleep, firm mattress, no loose bedding",
		"Well-child visit schedule per AAP recommendations",
	}

	// Age-specific vaccines and developmental surveillance.
	switch {
	case ageMonths == 2:
		plan.Medications = []MedicationEntry{
			{Name: "HepB", Dose: "0.5 mL", Frequency: "dose 2 of 3", Class: "vaccine"},
			{Name: "DTaP", Dose: "0.5 mL", Frequency: "dose 1 of 5", Class: "vaccine"},
			{Name: "Hib", Dose: "0.5 mL", Frequency: "dose 1 of 4", Class: "vaccine"},
			{Name: "PCV15", Dose: "0.5 mL", Frequency: "dose 1 of 4", Class: "vaccine"},
			{Name: "IPV", Dose: "0.5 mL", Frequency: "dose 1 of 4", Class: "vaccine"},
			{Name: "RV", Dose: "1-2 mL", Frequency: "dose 1 of 2-3", Class: "vaccine"},
		}
		plan.Education = append(plan.Education,
			"Developmental milestone: social smile (6-8 weeks)",
			"Tummy time 3-5 times daily to strengthen neck and shoulder muscles",
		)
		plan.NextReview = t.AddDate(0, 2, 0)

	case ageMonths == 4:
		plan.Medications = []MedicationEntry{
			{Name: "DTaP", Dose: "0.5 mL", Frequency: "dose 2 of 5", Class: "vaccine"},
			{Name: "Hib", Dose: "0.5 mL", Frequency: "dose 2 of 4", Class: "vaccine"},
			{Name: "PCV15", Dose: "0.5 mL", Frequency: "dose 2 of 4", Class: "vaccine"},
			{Name: "IPV", Dose: "0.5 mL", Frequency: "dose 2 of 4", Class: "vaccine"},
			{Name: "RV", Dose: "1-2 mL", Frequency: "dose 2 of 2-3", Class: "vaccine"},
		}
		plan.Education = append(plan.Education,
			"Developmental milestone: rolling from tummy to back",
			"Developmental milestone: tracks objects with eyes (visual tracking)",
			"Begin introducing high-allergen foods if no eczema/egg allergy per AAP guidance",
		)
		plan.NextReview = t.AddDate(0, 2, 0)

	case ageMonths == 6:
		plan.Medications = []MedicationEntry{
			{Name: "HepB", Dose: "0.5 mL", Frequency: "dose 3 of 3", Class: "vaccine"},
			{Name: "DTaP", Dose: "0.5 mL", Frequency: "dose 3 of 5", Class: "vaccine"},
			{Name: "Hib", Dose: "0.5 mL", Frequency: "dose 3 of 4", Class: "vaccine"},
			{Name: "PCV15", Dose: "0.5 mL", Frequency: "dose 3 of 4", Class: "vaccine"},
			{Name: "IPV", Dose: "0.5 mL", Frequency: "dose 3 of 4", Class: "vaccine"},
			{Name: "RV", Dose: "1-2 mL", Frequency: "dose 3 of 3 (if RotaTeq)", Class: "vaccine"},
			{Name: "Flu", Dose: "0.5 mL", Frequency: "annual (seasonal)", Class: "vaccine"},
		}
		plan.Education = append(plan.Education,
			"Developmental milestone: sitting with support",
			"Introduce solid foods: single-ingredient purees, one new food every 3-5 days",
			"Ibuprofen now appropriate for fever management",
		)
		plan.NextReview = t.AddDate(0, 6, 0)

	case ageMonths == 12:
		plan.Medications = []MedicationEntry{
			{Name: "MMR", Dose: "0.5 mL", Frequency: "dose 1 of 2", Class: "vaccine"},
			{Name: "Varicella", Dose: "0.5 mL", Frequency: "dose 1 of 2", Class: "vaccine"},
			{Name: "HepA", Dose: "0.5 mL", Frequency: "dose 1 of 2", Class: "vaccine"},
			{Name: "PCV15", Dose: "0.5 mL", Frequency: "booster (dose 4 of 4)", Class: "vaccine"},
		}
		plan.Education = append(plan.Education,
			"Developmental milestone: walking (9-12 months)",
			"Developmental milestone: first words (mama/dada specific by 12 months)",
			"Transition to whole cow's milk (up to 24 oz/day), reduce bottle use",
			"Lead screening if at risk; anemia screening",
		)
		plan.NextReview = t.AddDate(0, 3, 0)

	case ageMonths == 15:
		plan.Medications = []MedicationEntry{
			{Name: "DTaP", Dose: "0.5 mL", Frequency: "dose 4 of 5", Class: "vaccine"},
			{Name: "Hib", Dose: "0.5 mL", Frequency: "booster (dose 4 of 4)", Class: "vaccine"},
			{Name: "HepA", Dose: "0.5 mL", Frequency: "dose 2 of 2", Class: "vaccine"},
		}
		plan.Education = append(plan.Education,
			"Developmental milestone: vocabulary of 5-10 words",
			"Developmental milestone: walking independently",
			"Screen for autism (M-CHAT-R/F) if not done at 12 months",
			"Limit screen time; read aloud daily",
		)
		plan.NextReview = t.AddDate(0, 3, 0)

	case ageMonths == 18:
		plan.Medications = []MedicationEntry{
			{Name: "Flu", Dose: "0.5 mL", Frequency: "annual", Class: "vaccine"},
		}
		plan.Education = append(plan.Education,
			"Developmental milestone: 2-word phrases expected by 24 months",
			"M-CHAT-R/F autism screening at 18 months",
			"Dental home established; brush twice daily with rice-grain fluoride toothpaste",
			"Safe firearm storage counseling",
		)
		plan.NextReview = t.AddDate(0, 6, 0)

	case ageMonths == 24:
		plan.Medications = []MedicationEntry{
			{Name: "Flu", Dose: "0.5 mL", Frequency: "annual", Class: "vaccine"},
		}
		plan.Education = append(plan.Education,
			"Developmental screening: M-CHAT for autism spectrum disorder",
			"Developmental milestone: 50+ words, two-word phrases",
			"Transition from bottle; limit juice to 4 oz/day",
			"Forward-facing car seat when weight/height limits rear-facing seat exceeded",
		)
		plan.NextReview = t.AddDate(1, 0, 0)

	case ageMonths >= 48 && ageMonths <= 72: // 4-6 years
		plan.Medications = []MedicationEntry{
			{Name: "DTaP", Dose: "0.5 mL", Frequency: "dose 5 of 5 (booster)", Class: "vaccine"},
			{Name: "IPV", Dose: "0.5 mL", Frequency: "dose 4 of 4 (booster)", Class: "vaccine"},
			{Name: "MMR", Dose: "0.5 mL", Frequency: "dose 2 of 2 (booster)", Class: "vaccine"},
			{Name: "Varicella", Dose: "0.5 mL", Frequency: "dose 2 of 2 (booster)", Class: "vaccine"},
			{Name: "Flu", Dose: "0.5 mL", Frequency: "annual", Class: "vaccine"},
		}
		plan.Education = append(plan.Education,
			"School readiness assessment",
			"Vision and hearing screening before kindergarten",
			"Developmental milestone: can draw simple shapes, follow 3-step instructions",
			"Booster seat until seat belt fits properly (typically 4 ft 9 in)",
		)
		plan.NextReview = t.AddDate(1, 0, 0)

	case ageMonths >= 132 && ageMonths <= 144: // 11-12 years
		plan.Medications = []MedicationEntry{
			{Name: "Tdap", Dose: "0.5 mL", Frequency: "booster (replaces DTaP)", Class: "vaccine"},
			{Name: "HPV", Dose: "0.5 mL", Frequency: "dose 1 of 2 (2-dose series if <15 y)", Class: "vaccine"},
			{Name: "MenACWY", Dose: "0.5 mL", Frequency: "dose 1 of 2", Class: "vaccine"},
			{Name: "Flu", Dose: "0.5 mL", Frequency: "annual", Class: "vaccine"},
		}
		plan.Education = append(plan.Education,
			"Puberty counseling: Tanner staging, menstrual health, testicular self-exam",
			"HPV vaccine series counseling: cancer prevention, two doses 6-12 months apart",
			"Mental health screening: PHQ-A for adolescent depression",
			"Confidential visit component per HIPAA/FERPA for adolescent health",
			"Safe messaging around social media and online safety",
		)
		plan.NextReview = t.AddDate(1, 0, 0)

	case ageMonths >= 192 && ageMonths <= 204: // 16 years
		plan.Medications = []MedicationEntry{
			{Name: "MenACWY", Dose: "0.5 mL", Frequency: "booster (dose 2 of 2)", Class: "vaccine"},
			{Name: "HPV", Dose: "0.5 mL", Frequency: "complete series if not done", Class: "vaccine"},
			{Name: "Flu", Dose: "0.5 mL", Frequency: "annual", Class: "vaccine"},
		}
		plan.Education = append(plan.Education,
			"MenACWY booster at age 16 for meningococcal protection",
			"HPV completion counseling if series not complete",
			"Driving safety: graduated license laws, no phone while driving",
			"Substance use screening: CRAFFT or AUDIT-C",
			"Reproductive health counseling",
		)
		plan.NextReview = t.AddDate(1, 0, 0)

	default:
		// Generic well-child interval for ages not explicitly mapped above.
		plan.Education = append(plan.Education,
			"Follow AAP Bright Futures schedule for age-appropriate anticipatory guidance",
			"Ensure vaccines are current per ACIP schedule",
		)
		plan.NextReview = nextWellChildVisit(ageMonths, t)
	}

	return plan
}

// nextWellChildVisit returns the recommended next well-child visit time
// based on the current age in months for ages not explicitly mapped.
func nextWellChildVisit(ageMonths int, t time.Time) time.Time {
	switch {
	case ageMonths < 6:
		return t.AddDate(0, 2, 0)
	case ageMonths < 18:
		return t.AddDate(0, 3, 0)
	case ageMonths < 36:
		return t.AddDate(0, 6, 0)
	default:
		return t.AddDate(1, 0, 0)
	}
}
