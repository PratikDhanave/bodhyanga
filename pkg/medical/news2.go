package medical

import "strings"

// NEWS2Input holds the raw vital-sign readings required to calculate the
// National Early Warning Score 2 (NEWS2) as defined by the Royal College of
// Physicians (2017).
//
// Consciousness values: "alert" | "confused" | "voice" | "pain" | "unresponsive"
// COPD true selects SpO2 Scale 2 (hypercapnic respiratory failure targets).
type NEWS2Input struct {
	RespiratoryRate int
	SpO2            float64
	OnO2            bool
	COPD            bool
	SystolicBP      int
	HeartRate       int
	Consciousness   string  // "alert" | "confused" | "voice" | "pain" | "unresponsive"
	Temperature     float64 // degrees Celsius
}

// NEWS2Result is the scored output of CalculateNEWS2.
type NEWS2Result struct {
	Total          int            // 0–20
	Components     map[string]int // component name → individual score
	RiskLevel      string         // "low" | "low-medium" | "medium" | "high"
	Recommendation string
	Escalate       bool // true when immediate clinical response is required
}

// CalculateNEWS2 scores an observation set according to the NEWS2 chart.
//
// Risk stratification (RCP 2017):
//   - 0–4           → low
//   - 5–6           → low-medium (also triggered by any single score ≥ 3)
//   - 7+            → high
func CalculateNEWS2(input NEWS2Input) NEWS2Result {
	components := make(map[string]int, 8)

	// --- Respiratory rate ---
	rrScore := respiratoryRateScore(input.RespiratoryRate)
	components["respiratory_rate"] = rrScore

	// --- SpO2 ---
	var spo2Score int
	if input.COPD {
		spo2Score = spo2Scale2Score(input.SpO2)
	} else {
		spo2Score = spo2Scale1Score(input.SpO2)
	}
	components["spo2"] = spo2Score

	// --- Supplemental O2 ---
	o2Score := 0
	if input.OnO2 {
		o2Score = 2
	}
	components["on_o2"] = o2Score

	// --- Systolic BP ---
	sbpScore := systolicBPScore(input.SystolicBP)
	components["systolic_bp"] = sbpScore

	// --- Heart rate ---
	hrScore := heartRateScore(input.HeartRate)
	components["heart_rate"] = hrScore

	// --- Consciousness ---
	consScore := consciousnessScore(input.Consciousness)
	components["consciousness"] = consScore

	// --- Temperature ---
	tempScore := temperatureScore(input.Temperature)
	components["temperature"] = tempScore

	// Total
	total := 0
	for _, v := range components {
		total += v
	}

	// Risk level — single-parameter ≥3 triggers low-medium even when total < 5
	hasSingleHigh := false
	for _, v := range components {
		if v >= 3 {
			hasSingleHigh = true
			break
		}
	}

	var riskLevel string
	var recommendation string
	var escalate bool

	switch {
	case total >= 7:
		riskLevel = "high"
		recommendation = "Urgent clinical review. Consider HDU/ICU. Continuous monitoring required."
		escalate = true
	case total >= 5 || hasSingleHigh:
		riskLevel = "low-medium"
		recommendation = "Increased monitoring frequency. Senior nurse/doctor review within 30 minutes."
		escalate = false
	default:
		riskLevel = "low"
		recommendation = "Routine monitoring. Reassess per ward protocol (minimum 12-hourly)."
		escalate = false
	}

	return NEWS2Result{
		Total:          total,
		Components:     components,
		RiskLevel:      riskLevel,
		Recommendation: recommendation,
		Escalate:       escalate,
	}
}

// --- component scoring helpers ---

func respiratoryRateScore(rr int) int {
	switch {
	case rr <= 8:
		return 3
	case rr <= 11:
		return 1
	case rr <= 20:
		return 0
	case rr <= 24:
		return 2
	default: // ≥ 25
		return 3
	}
}

// spo2Scale1Score applies SpO2 Scale 1 (standard / no hypercapnic RF).
func spo2Scale1Score(spo2 float64) int {
	switch {
	case spo2 <= 91:
		return 3
	case spo2 <= 93:
		return 2
	case spo2 <= 95:
		return 1
	default: // ≥ 96
		return 0
	}
}

// spo2Scale2Score applies SpO2 Scale 2 (COPD / hypercapnic respiratory failure).
// Target range 88–92 % is normal for this population.
//
// Scale 2 thresholds (RCP NEWS2 2017):
//
//	≤83 % → 3  |  84–85 % → 2  |  86–87 % → 1
//	88–92 % → 0  |  93–94 % → 1  |  95–96 % → 2  |  ≥97 % → 3
func spo2Scale2Score(spo2 float64) int {
	switch {
	case spo2 <= 83:
		return 3
	case spo2 <= 85:
		return 2
	case spo2 <= 87:
		return 1
	case spo2 <= 92:
		return 0
	case spo2 <= 94:
		return 1
	case spo2 <= 96:
		return 2
	default: // ≥ 97
		return 3
	}
}

func systolicBPScore(sbp int) int {
	switch {
	case sbp <= 90:
		return 3
	case sbp <= 100:
		return 2
	case sbp <= 110:
		return 1
	case sbp <= 219:
		return 0
	default: // ≥ 220
		return 3
	}
}

func heartRateScore(hr int) int {
	switch {
	case hr <= 40:
		return 3
	case hr <= 50:
		return 1
	case hr <= 90:
		return 0
	case hr <= 110:
		return 1
	case hr <= 130:
		return 2
	default: // ≥ 131
		return 3
	}
}

func consciousnessScore(c string) int {
	if strings.ToLower(c) == "alert" {
		return 0
	}
	return 3
}

func temperatureScore(temp float64) int {
	switch {
	case temp <= 35.0:
		return 3
	case temp <= 36.0:
		return 1
	case temp <= 38.0:
		return 0
	case temp <= 39.0:
		return 1
	default: // ≥ 39.1
		return 2
	}
}
