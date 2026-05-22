package medical

import "testing"

func TestEncodeDecodeState(t *testing.T) {
	orig := CaseState{
		CaseID:       "c-1",
		Presentation: "cough",
		MaxCostUSD:   500,
		TotalCostUSD: 220,
		Hypotheses:   []string{"pneumonia"},
	}
	encoded := EncodeState(orig)
	decoded, err := DecodeState(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.CaseID != orig.CaseID || decoded.TotalCostUSD != orig.TotalCostUSD {
		t.Fatalf("round-trip mismatch: %+v vs %+v", decoded, orig)
	}
}
