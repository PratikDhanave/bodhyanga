package agents

import "testing"

// The simulated Lab prices each investigation from the catalog (specs/006).
func TestSimulatedLab_PricesTests(t *testing.T) {
	cases := map[string]float64{
		"troponin":   45,
		"ecg":        30,
		"chest xray": 80,
		"unknown":    0,
	}
	for test, want := range cases {
		if got := SimulatedLab(test, PatientCase{}).CostUSD; got != want {
			t.Errorf("SimulatedLab(%q).CostUSD = %.0f, want %.0f", test, got, want)
		}
	}
}

func TestTotalCostUSD_Sums(t *testing.T) {
	results := []TestResult{{CostUSD: 45}, {CostUSD: 30}, {CostUSD: 0}}
	if got := totalCostUSD(results); got != 75 {
		t.Errorf("totalCostUSD = %.0f, want 75", got)
	}
	if got := totalCostUSD(nil); got != 0 {
		t.Errorf("totalCostUSD(nil) = %.0f, want 0", got)
	}
}
