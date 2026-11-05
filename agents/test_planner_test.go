package agents

import (
	"context"
	"sort"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// TestTestPlannerDispatchesWorkupByCondition verifies the planner orders a
// condition-appropriate test set instead of the historical respiratory-only
// workup. SD-Bench surfaced that flat behaviour as the "Mean Cost flat across
// cases" symptom; this test guards the fix.
func TestTestPlannerDispatchesWorkupByCondition(t *testing.T) {
	cases := []struct {
		condition string
		want      []string
	}{
		{
			condition: "cap",
			want:      []string{"complete_blood_count", "chest_xray", "basic_metabolic_panel"},
		},
		{
			condition: "chf",
			want:      []string{"bnp", "chest_xray", "ecg", "basic_metabolic_panel"},
		},
		{
			condition: "acs",
			want:      []string{"troponin_i", "ecg", "basic_metabolic_panel", "complete_blood_count"},
		},
		{
			condition: "sepsis",
			want:      []string{"blood_culture", "lactate", "complete_blood_count", "basic_metabolic_panel", "urinalysis"},
		},
		{
			condition: "dka",
			want:      []string{"basic_metabolic_panel", "urine_ketones", "arterial_blood_gas", "complete_blood_count"},
		},
		{
			// Unknown condition falls back to the historical default workup.
			condition: "unknown_condition",
			want:      []string{"complete_blood_count", "chest_xray", "basic_metabolic_panel"},
		},
		{
			// Empty condition (legacy intake without condition tag) falls back
			// to default — guards backward compatibility.
			condition: "",
			want:      []string{"complete_blood_count", "chest_xray", "basic_metabolic_panel"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run("condition="+tc.condition, func(t *testing.T) {
			state := medical.CaseState{
				CaseID:    "t1",
				Condition: tc.condition,
			}
			msg := agent.NewMessage("diagnostic_supervisor", "test_planner", agent.RoleAgent,
				"case_state", medical.EncodeState(state), map[string]any{"case_id": "t1"})

			out, err := (&TestPlannerAgent{}).HandleMessage(context.Background(), msg, newFakeEnv())
			if err != nil {
				t.Fatalf("HandleMessage err: %v", err)
			}
			if len(out) != 1 {
				t.Fatalf("want 1 outbound msg, got %d", len(out))
			}
			if out[0].To != "cost_guardian" {
				t.Errorf("want To=cost_guardian, got %q", out[0].To)
			}
			got, err := medical.DecodeState(out[0].Content)
			if err != nil {
				t.Fatalf("decode state: %v", err)
			}
			gotNames := make([]string, 0, len(got.TestsOrdered))
			for _, dt := range got.TestsOrdered {
				gotNames = append(gotNames, dt.Name)
			}
			if !equalUnordered(gotNames, tc.want) {
				t.Errorf("condition %q: tests ordered = %v, want %v", tc.condition, gotNames, tc.want)
			}
			// Every ordered test must carry a non-zero cost so the bench cost
			// axis isn't flat — that was the second SD-Bench symptom.
			for _, dt := range got.TestsOrdered {
				if dt.CostUSD <= 0 {
					t.Errorf("test %q has cost %.2f (want > 0); is it in TestCatalog?", dt.Name, dt.CostUSD)
				}
			}
			// TestResults must be populated for every ordered test so the
			// diagnostician has something to reason over.
			for _, dt := range got.TestsOrdered {
				if _, ok := got.TestResults[dt.Name]; !ok {
					t.Errorf("missing simulated result for %q", dt.Name)
				}
			}
		})
	}
}

// TestSimulatedResultsAreConditionAware confirms the per-test result strings
// vary with the case's condition — without this the diagnostician's
// condition-specific thresholds never fire.
func TestSimulatedResultsAreConditionAware(t *testing.T) {
	if got := simulatedTestResult("bnp", "chf"); got == simulatedTestResult("bnp", "cap") {
		t.Errorf("expected BNP result to differ for chf vs cap; got %q for both", got)
	}
	if got := simulatedTestResult("troponin_i", "acs"); got == simulatedTestResult("troponin_i", "cap") {
		t.Errorf("expected troponin result to differ for acs vs cap; got %q for both", got)
	}
	if got := simulatedTestResult("lactate", "sepsis"); got == simulatedTestResult("lactate", "cap") {
		t.Errorf("expected lactate result to differ for sepsis vs cap; got %q for both", got)
	}
	if got := simulatedTestResult("urine_ketones", "dka"); got == simulatedTestResult("urine_ketones", "cap") {
		t.Errorf("expected urine_ketones result to differ for dka vs cap; got %q for both", got)
	}
}

func equalUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}
