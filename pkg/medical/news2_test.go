package medical

import (
	"testing"
)

func TestNEWS2(t *testing.T) {
	tests := []struct {
		name          string
		input         NEWS2Input
		wantTotal     int
		wantRisk      string
		wantEscalate  bool
		wantComponent map[string]int // spot-check specific components; nil = skip
	}{
		{
			name: "all normal — low risk",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    0,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"respiratory_rate": 0,
				"spo2":             0,
				"on_o2":            0,
				"systolic_bp":      0,
				"heart_rate":       0,
				"consciousness":    0,
				"temperature":      0,
			},
		},
		{
			name: "high risk — all extremes (total >= 7)",
			input: NEWS2Input{
				RespiratoryRate: 30,  // score 3
				SpO2:            88,  // score 3 (scale 1)
				OnO2:            true, // score 2
				COPD:            false,
				SystolicBP:      85,  // score 3
				HeartRate:       135, // score 3
				Consciousness:   "unresponsive", // score 3
				Temperature:     34.5, // score 3
			},
			wantTotal:    20,
			wantRisk:     "high",
			wantEscalate: true,
			wantComponent: map[string]int{
				"respiratory_rate": 3,
				"spo2":             3,
				"on_o2":            2,
				"systolic_bp":      3,
				"heart_rate":       3,
				"consciousness":    3,
				"temperature":      3,
			},
		},
		{
			name: "respiratory rate extremes — rr=8 scores 3",
			input: NEWS2Input{
				RespiratoryRate: 8,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    3,
			wantRisk:     "low-medium", // single score ≥ 3
			wantEscalate: false,
			wantComponent: map[string]int{
				"respiratory_rate": 3,
			},
		},
		{
			name: "respiratory rate 9-11 scores 1",
			input: NEWS2Input{
				RespiratoryRate: 10,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    1,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"respiratory_rate": 1,
			},
		},
		{
			name: "respiratory rate 21-24 scores 2",
			input: NEWS2Input{
				RespiratoryRate: 22,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    2,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"respiratory_rate": 2,
			},
		},
		{
			name: "SpO2 scale 1 boundary 94% scores 1",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            94,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    1,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"spo2": 1,
			},
		},
		{
			name: "SpO2 scale 1 <= 91% scores 3",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            90,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    3,
			wantRisk:     "low-medium",
			wantEscalate: false,
			wantComponent: map[string]int{
				"spo2": 3,
			},
		},
		{
			name: "SpO2 scale 2 COPD — 90% is normal (score 0)",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            90,
				OnO2:            false,
				COPD:            true,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    0,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"spo2": 0,
			},
		},
		{
			name: "SpO2 scale 2 COPD — 97% scores 3 (too high)",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            97,
				OnO2:            false,
				COPD:            true,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    3,
			wantRisk:     "low-medium",
			wantEscalate: false,
			wantComponent: map[string]int{
				"spo2": 3,
			},
		},
		{
			name: "on supplemental O2 adds 2",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            true,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    2,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"on_o2": 2,
			},
		},
		{
			name: "systolic BP <= 90 scores 3",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      88,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    3,
			wantRisk:     "low-medium",
			wantEscalate: false,
			wantComponent: map[string]int{
				"systolic_bp": 3,
			},
		},
		{
			name: "systolic BP >= 220 scores 3",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      225,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    3,
			wantRisk:     "low-medium",
			wantEscalate: false,
			wantComponent: map[string]int{
				"systolic_bp": 3,
			},
		},
		{
			name: "systolic BP 101-110 scores 1",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      105,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    1,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"systolic_bp": 1,
			},
		},
		{
			name: "heart rate <= 40 scores 3",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       38,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    3,
			wantRisk:     "low-medium",
			wantEscalate: false,
			wantComponent: map[string]int{
				"heart_rate": 3,
			},
		},
		{
			name: "heart rate 41-50 scores 1",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       45,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    1,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"heart_rate": 1,
			},
		},
		{
			name: "heart rate 111-130 scores 2",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       120,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    2,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"heart_rate": 2,
			},
		},
		{
			name: "consciousness confused scores 3",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "confused",
				Temperature:     37.0,
			},
			wantTotal:    3,
			wantRisk:     "low-medium",
			wantEscalate: false,
			wantComponent: map[string]int{
				"consciousness": 3,
			},
		},
		{
			name: "temperature <= 35.0 scores 3",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     34.9,
			},
			wantTotal:    3,
			wantRisk:     "low-medium",
			wantEscalate: false,
			wantComponent: map[string]int{
				"temperature": 3,
			},
		},
		{
			name: "temperature 35.1-36.0 scores 1",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     35.5,
			},
			wantTotal:    1,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"temperature": 1,
			},
		},
		{
			name: "temperature 38.1-39.0 scores 1",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     38.5,
			},
			wantTotal:    1,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"temperature": 1,
			},
		},
		{
			name: "temperature >= 39.1 scores 2",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     39.5,
			},
			wantTotal:    2,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"temperature": 2,
			},
		},
		{
			name: "total 5 without single score 3 — low-medium",
			input: NEWS2Input{
				RespiratoryRate: 22, // 2
				SpO2:            94, // 1
				OnO2:            true, // 2
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    5,
			wantRisk:     "low-medium",
			wantEscalate: false,
		},
		{
			name: "high risk escalate — total 7",
			input: NEWS2Input{
				RespiratoryRate: 30,  // 3
				SpO2:            92,  // 2
				OnO2:            false,
				COPD:            false,
				SystolicBP:      100, // 2
				HeartRate:       75,
				Consciousness:   "alert",
				Temperature:     37.0,
			},
			wantTotal:    7,
			wantRisk:     "high",
			wantEscalate: true,
		},
		{
			name: "consciousness case-insensitive ALERT scores 0",
			input: NEWS2Input{
				RespiratoryRate: 16,
				SpO2:            98,
				OnO2:            false,
				COPD:            false,
				SystolicBP:      120,
				HeartRate:       75,
				Consciousness:   "ALERT",
				Temperature:     37.0,
			},
			wantTotal:    0,
			wantRisk:     "low",
			wantEscalate: false,
			wantComponent: map[string]int{
				"consciousness": 0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CalculateNEWS2(tc.input)

			if result.Total != tc.wantTotal {
				t.Errorf("Total: got %d, want %d", result.Total, tc.wantTotal)
			}
			if result.RiskLevel != tc.wantRisk {
				t.Errorf("RiskLevel: got %q, want %q", result.RiskLevel, tc.wantRisk)
			}
			if result.Escalate != tc.wantEscalate {
				t.Errorf("Escalate: got %v, want %v", result.Escalate, tc.wantEscalate)
			}
			if result.Recommendation == "" {
				t.Error("Recommendation must not be empty")
			}

			for comp, wantScore := range tc.wantComponent {
				gotScore, ok := result.Components[comp]
				if !ok {
					t.Errorf("component %q missing from result", comp)
					continue
				}
				if gotScore != wantScore {
					t.Errorf("component %q: got %d, want %d", comp, gotScore, wantScore)
				}
			}
		})
	}
}
