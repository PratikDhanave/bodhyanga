package medical

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var pediatricTestTime = time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

// TestPediatricWellChildCarePlan_FixedVisits checks each explicitly-mapped
// age milestone for correct vaccines and developmental notes.
func TestPediatricWellChildCarePlan_FixedVisits(t *testing.T) {
	cases := []struct {
		ageMonths     int
		wantVaccines  []string
		wantEdKeyword string
	}{
		{
			ageMonths:     2,
			wantVaccines:  []string{"HepB", "DTaP", "Hib", "PCV15", "IPV", "RV"},
			wantEdKeyword: "social smile",
		},
		{
			ageMonths:     4,
			wantVaccines:  []string{"DTaP", "Hib", "PCV15", "IPV", "RV"},
			wantEdKeyword: "rolling",
		},
		{
			ageMonths:     6,
			wantVaccines:  []string{"HepB", "DTaP", "Hib", "PCV15", "IPV", "RV", "Flu"},
			wantEdKeyword: "sitting with support",
		},
		{
			ageMonths:     12,
			wantVaccines:  []string{"MMR", "Varicella", "HepA", "PCV15"},
			wantEdKeyword: "first words",
		},
		{
			ageMonths:     15,
			wantVaccines:  []string{"DTaP", "Hib", "HepA"},
			wantEdKeyword: "5-10 words",
		},
		{
			ageMonths:     18,
			wantVaccines:  []string{"Flu"},
			wantEdKeyword: "2-word phrases",
		},
		{
			ageMonths:     24,
			wantVaccines:  []string{"Flu"},
			wantEdKeyword: "M-CHAT",
		},
		{
			ageMonths:     60, // 5 years — inside 4-6 year window
			wantVaccines:  []string{"DTaP", "IPV", "MMR", "Varicella"},
			wantEdKeyword: "school readiness",
		},
		{
			ageMonths:     132, // 11 years — inside 11-12 year window
			wantVaccines:  []string{"Tdap", "HPV", "MenACWY"},
			wantEdKeyword: "puberty",
		},
		{
			ageMonths:     192, // 16 years
			wantVaccines:  []string{"MenACWY", "HPV"},
			wantEdKeyword: "booster",
		},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("age_%dm", c.ageMonths), func(t *testing.T) {
			plan := BuildPediatricWellChildCarePlan("case-1", "pt-1", c.ageMonths, 8.0, pediatricTestTime)

			if plan.Condition != "well_child" {
				t.Errorf("condition: want well_child, got %q", plan.Condition)
			}

			// Check required vaccines appear in medications list.
			vaccineSet := make(map[string]bool)
			for _, m := range plan.Medications {
				vaccineSet[m.Name] = true
			}
			for _, v := range c.wantVaccines {
				if !vaccineSet[v] {
					t.Errorf("age %dm: missing vaccine %q; medications: %v", c.ageMonths, v, plan.Medications)
				}
			}

			// Check that all vaccines have class "vaccine".
			for _, m := range plan.Medications {
				if m.Class != "vaccine" {
					t.Errorf("age %dm: medication %q has class %q, want vaccine", c.ageMonths, m.Name, m.Class)
				}
			}

			// Check developmental/education keyword.
			joined := strings.Join(plan.Education, " ")
			if !strings.Contains(strings.ToLower(joined), strings.ToLower(c.wantEdKeyword)) {
				t.Errorf("age %dm: education missing keyword %q; education: %v", c.ageMonths, c.wantEdKeyword, plan.Education)
			}

			// NextReview must be in the future.
			if !plan.NextReview.After(pediatricTestTime) {
				t.Errorf("age %dm: next review %s is not after now %s", c.ageMonths, plan.NextReview, pediatricTestTime)
			}
		})
	}
}

// TestPediatricWellChildCarePlan_StandardGoals verifies that every visit has
// the four standard goals (growth curve + milestones + vaccines current).
func TestPediatricWellChildCarePlan_StandardGoals(t *testing.T) {
	ages := []int{2, 4, 6, 12, 15, 18, 24, 60, 132, 192}
	wantMetrics := []string{
		"weight_on_growth_curve",
		"height_on_growth_curve",
		"developmental_milestones_met",
		"vaccines_current",
	}

	for _, age := range ages {
		plan := BuildPediatricWellChildCarePlan("case-goals", "pt-goals", age, 10.0, pediatricTestTime)

		goalSet := make(map[string]bool)
		for _, g := range plan.Goals {
			goalSet[g.Metric] = true
		}
		for _, m := range wantMetrics {
			if !goalSet[m] {
				t.Errorf("age %dm: missing goal metric %q", age, m)
			}
		}
	}
}

// TestPediatricWellChildCarePlan_WeightBasedDosing validates that the
// antipyretic dosing note reflects the provided weight.
func TestPediatricWellChildCarePlan_WeightBasedDosing(t *testing.T) {
	cases := []struct {
		weightKg     float64
		wantAcetoMin float64 // 10 mg/kg
		wantAcetoMax float64 // 15 mg/kg
		wantIbuMin   float64 // 5 mg/kg
		wantIbuMax   float64 // 10 mg/kg
	}{
		{weightKg: 5.0, wantAcetoMin: 50, wantAcetoMax: 75, wantIbuMin: 25, wantIbuMax: 50},
		{weightKg: 10.0, wantAcetoMin: 100, wantAcetoMax: 150, wantIbuMin: 50, wantIbuMax: 100},
		{weightKg: 20.0, wantAcetoMin: 200, wantAcetoMax: 300, wantIbuMin: 100, wantIbuMax: 200},
	}

	for _, c := range cases {
		plan := BuildPediatricWellChildCarePlan("case-dose", "pt-dose", 6, c.weightKg, pediatricTestTime)

		joined := strings.Join(plan.Education, " ")
		// Acetaminophen min dose should appear.
		acetoMinStr := fmt.Sprintf("%.0f", c.wantAcetoMin)
		if !strings.Contains(joined, acetoMinStr) {
			t.Errorf("weight %.1f kg: education missing acetaminophen min dose %s mg; got: %v",
				c.weightKg, acetoMinStr, plan.Education)
		}

		// Ibuprofen max dose should appear.
		ibuMaxStr := fmt.Sprintf("%.0f", c.wantIbuMax)
		if !strings.Contains(joined, ibuMaxStr) {
			t.Errorf("weight %.1f kg: education missing ibuprofen max dose %s mg; got: %v",
				c.weightKg, ibuMaxStr, plan.Education)
		}
	}
}

// TestPediatricWellChildCarePlan_Monitoring verifies standard monitoring entries
// are present for all visits.
func TestPediatricWellChildCarePlan_Monitoring(t *testing.T) {
	wantMetrics := []string{"weight_kg", "height_cm", "bmi_percentile", "blood_pressure"}

	plan := BuildPediatricWellChildCarePlan("case-mon", "pt-mon", 12, 9.5, pediatricTestTime)

	monSet := make(map[string]bool)
	for _, m := range plan.Monitoring {
		monSet[m.Metric] = true
	}
	for _, m := range wantMetrics {
		if !monSet[m] {
			t.Errorf("missing monitoring metric %q; monitoring: %v", m, plan.Monitoring)
		}
	}
}

// TestPediatricWellChildCarePlan_DefaultAge tests an age that falls into the
// default case (e.g., 36 months / 3 years) to ensure it still produces a
// valid plan with a future NextReview.
func TestPediatricWellChildCarePlan_DefaultAge(t *testing.T) {
	plan := BuildPediatricWellChildCarePlan("case-default", "pt-default", 36, 14.0, pediatricTestTime)

	if plan.Condition != "well_child" {
		t.Errorf("condition: want well_child, got %q", plan.Condition)
	}
	if !plan.NextReview.After(pediatricTestTime) {
		t.Errorf("next review must be in the future; got %s", plan.NextReview)
	}
	if len(plan.Goals) == 0 {
		t.Error("expected at least one goal")
	}
}

// TestPediatricWellChildCarePlan_RoundTrip ensures the plan survives JSON
// encode/decode (exercises the shared CarePlan codec).
func TestPediatricWellChildCarePlan_RoundTrip(t *testing.T) {
	orig := BuildPediatricWellChildCarePlan("case-rt", "pt-rt", 12, 9.0, pediatricTestTime)
	got, err := DecodeCarePlan(EncodeCarePlan(orig))
	if err != nil {
		t.Fatal(err)
	}
	if got.Condition != "well_child" || got.PatientID != "pt-rt" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if len(got.Goals) != len(orig.Goals) {
		t.Errorf("goals lost in round trip: want %d, got %d", len(orig.Goals), len(got.Goals))
	}
}
