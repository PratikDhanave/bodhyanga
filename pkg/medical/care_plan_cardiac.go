package medical

import "time"

// BuildPostMICarePlan returns a deterministic care plan for post-myocardial
// infarction management following AHA/ACC secondary-prevention guidelines.
// Illustrative thresholds only — educational demo, not clinical guideline.
func BuildPostMICarePlan(caseID, patientID string, t time.Time) CarePlan {
	now := t.UTC()
	return CarePlan{
		CaseID:    caseID,
		PatientID: patientID,
		Condition: "post_mi",
		CreatedAt: now,
		Goals: []GoalEntry{
			{Metric: "ldl", Target: 70, Unit: "mg/dL", Window: "90d"},
			{Metric: "systolic_bp", Target: 130, Unit: "mmHg", Window: "90d"},
			{Metric: "smoking_status", Target: 0, Unit: "cigarettes/day", Window: "30d"},
			{Metric: "cardiac_rehab_enrolled", Target: 1, Unit: "boolean", Window: "30d"},
		},
		Medications: []MedicationEntry{
			{Name: "Aspirin", Dose: "81 mg", Frequency: "daily", Class: "antiplatelet"},
			{Name: "Atorvastatin", Dose: "40-80 mg", Frequency: "daily", Class: "high_intensity_statin"},
			{Name: "Metoprolol succinate", Dose: "25-200 mg", Frequency: "daily", Class: "beta_blocker"},
			{Name: "Lisinopril", Dose: "5-40 mg", Frequency: "daily", Class: "ace_inhibitor"},
			{Name: "Clopidogrel", Dose: "75 mg", Frequency: "daily x 12 months", Class: "p2y12_antiplatelet"},
		},
		Monitoring: []MonitoringEntry{
			{Metric: "heart_rate", Cadence: "daily", Source: "rpm", ThresholdMin: 50, ThresholdMax: 100},
			{Metric: "systolic_bp", Cadence: "daily", Source: "rpm", ThresholdMax: 160},
			{Metric: "ldl", Cadence: "monthly", Source: "lab"},
		},
		Education: []string{
			"Cardiac rehabilitation enrollment",
			"Heart-healthy diet (Mediterranean / DASH)",
			"Smoking cessation",
			"Medication adherence — dual antiplatelet for 12 months",
			"When to call 911: chest pain, shortness of breath",
		},
		// Follow-up: 2 weeks, 3 months, 6 months, 12 months — closest is 2 weeks.
		NextReview: now.AddDate(0, 0, 14),
	}
}

// BuildPostStrokeCarePlan returns a deterministic care plan for post-stroke
// secondary prevention. strokeType must be "ischemic" or "hemorrhagic".
// Illustrative thresholds only — educational demo, not clinical guideline.
func BuildPostStrokeCarePlan(caseID, patientID string, strokeType string, t time.Time) CarePlan {
	now := t.UTC()
	plan := CarePlan{
		CaseID:    caseID,
		PatientID: patientID,
		CreatedAt: now,
		Goals: []GoalEntry{
			{Metric: "systolic_bp", Target: 130, Unit: "mmHg", Window: "90d"},
			{Metric: "smoking_status", Target: 0, Unit: "cigarettes/day", Window: "30d"},
			{Metric: "rehab_therapy_enrolled", Target: 1, Unit: "boolean", Window: "14d"},
		},
		Monitoring: []MonitoringEntry{
			{Metric: "systolic_bp", Cadence: "daily", Source: "rpm", ThresholdMax: 160},
			{Metric: "neurological_status", Cadence: "weekly", Source: "self_report"},
		},
		Education: []string{
			"Stroke warning signs (FAST: Face, Arm, Speech, Time)",
			"Blood pressure self-monitoring",
			"Physical / occupational / speech therapy participation",
			"Smoking cessation",
			"When to call 911: sudden numbness, vision loss, severe headache",
		},
	}

	switch strokeType {
	case "hemorrhagic":
		plan.Condition = "post_stroke_hemorrhagic"
		plan.Medications = []MedicationEntry{
			// NO anticoagulants for hemorrhagic stroke.
			{Name: "Amlodipine", Dose: "5-10 mg", Frequency: "daily", Class: "calcium_channel_blocker"},
		}
		plan.Education = append(plan.Education, "Neurosurgery follow-up as scheduled")
		plan.NextReview = now.AddDate(0, 0, 14)
	default: // "ischemic"
		plan.Condition = "post_stroke_ischemic"
		plan.Medications = []MedicationEntry{
			// Aspirin is primary antiplatelet; clopidogrel is an alternative.
			{Name: "Aspirin", Dose: "81 mg", Frequency: "daily", Class: "antiplatelet"},
			{Name: "Atorvastatin", Dose: "40-80 mg", Frequency: "daily", Class: "high_intensity_statin"},
			{Name: "Lisinopril", Dose: "5-40 mg", Frequency: "daily", Class: "ace_inhibitor"},
			// Apixaban added when the stroke is afib-related (anticoagulation for cardioembolic source).
			{Name: "Apixaban", Dose: "5 mg", Frequency: "BID (if afib-related)", Class: "anticoagulant_noac"},
		}
		plan.NextReview = now.AddDate(0, 0, 14)
	}

	return plan
}
