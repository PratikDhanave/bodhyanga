package bench

import (
	"context"
	"reflect"
	"testing"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

func TestRunSuite_BenignCaseReachesPlanAndScores(t *testing.T) {
	fixtures := []Fixture{
		{ID: "acs", Gold: "acute coronary syndrome", Case: agents.PatientCase{CaseID: "c1", Symptoms: []string{"chest pain"}}},
		{ID: "chf", Gold: "heart failure", Case: agents.PatientCase{CaseID: "c2", Symptoms: []string{"shortness of breath"}}},
	}
	results, err := RunSuite(context.Background(), fixtures, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != StatusMatch {
			t.Errorf("case %s: status %q produced %q, want match", r.ID, r.Status, r.Produced)
		}
	}
}

// Per-case cost flows from the priced Lab through to the result (specs/006):
// a chest-pain case runs troponin ($45) + ecg ($30) = $75.
func TestRunSuite_CarriesPerCaseCost(t *testing.T) {
	fixtures := []Fixture{
		{ID: "acs", Gold: "acute coronary syndrome", Case: agents.PatientCase{CaseID: "c", Symptoms: []string{"chest pain"}}},
	}
	results, err := RunSuite(context.Background(), fixtures, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := results[0].CostUSD; got != 75 {
		t.Errorf("cost = $%.0f, want $75 (troponin 45 + ecg 30)", got)
	}
	if s := Aggregate(results); s.MeanCostUSD != 75 {
		t.Errorf("mean cost = $%.0f, want $75", s.MeanCostUSD)
	}
}

// Two runs of the same suite are identical (determinism, SC-002).
func TestRunSuite_Deterministic(t *testing.T) {
	fixtures := []Fixture{
		{ID: "cap", Gold: "community-acquired pneumonia", Case: agents.PatientCase{CaseID: "c", Symptoms: []string{"fever", "cough"}}},
		{ID: "undiff", Gold: "undifferentiated presentation", Case: agents.PatientCase{CaseID: "d", Symptoms: []string{"rash"}}},
	}
	a, err := RunSuite(context.Background(), fixtures, Config{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := RunSuite(context.Background(), fixtures, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("non-deterministic results:\n%+v\n%+v", a, b)
	}
}
