package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// invokeDrugInteraction is a small helper that runs the tool with the
// supplied JSON args and decodes the result into a drugInteractionOutput.
// Keeps each test below short and focused on its assertion.
func invokeDrugInteraction(t *testing.T, args string) drugInteractionOutput {
	t.Helper()
	raw, err := DrugInteraction{}.Invoke(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var out drugInteractionOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, string(raw))
	}
	return out
}

func TestDrugInteraction_KnownPairDetected(t *testing.T) {
	out := invokeDrugInteraction(t, `{"drugs":["lisinopril","spironolactone"]}`)
	if !out.Checked {
		t.Fatalf("checked=false, want true")
	}
	if len(out.Interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(out.Interactions))
	}
	hit := out.Interactions[0]
	// Pair is normalized to lexicographic order.
	if hit.Pair[0] != "lisinopril" || hit.Pair[1] != "spironolactone" {
		t.Fatalf("pair=%v, want sorted [lisinopril,spironolactone]", hit.Pair)
	}
	if hit.Severity == "" || hit.Mechanism == "" || hit.Management == "" {
		t.Fatalf("missing severity/mechanism/management: %+v", hit)
	}
	if got, ok := out.Metadata["source"]; !ok || got != "demo-synth" {
		t.Fatalf("metadata.source=%v, want demo-synth", out.Metadata["source"])
	}
}

func TestDrugInteraction_OrderIndependent(t *testing.T) {
	// Reversing the input order must produce the same hit.
	out := invokeDrugInteraction(t, `{"drugs":["spironolactone","lisinopril"]}`)
	if len(out.Interactions) != 1 {
		t.Fatalf("expected 1 interaction with reversed inputs, got %d", len(out.Interactions))
	}
}

func TestDrugInteraction_UnknownPairReturnsEmpty(t *testing.T) {
	out := invokeDrugInteraction(t, `{"drugs":["ibuprofen","loratadine"]}`)
	if !out.Checked {
		t.Fatalf("checked=false, want true")
	}
	if len(out.Interactions) != 0 {
		t.Fatalf("expected no interactions for unknown pair, got %d", len(out.Interactions))
	}
	if got := out.Metadata["pairs_scanned"]; got != float64(1) {
		t.Fatalf("pairs_scanned=%v, want 1", got)
	}
}

func TestDrugInteraction_BadJSONErrors(t *testing.T) {
	_, err := DrugInteraction{}.Invoke(context.Background(), json.RawMessage("not json"))
	if err == nil {
		t.Fatalf("expected error on malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Fatalf("error=%q, want decode mention", err.Error())
	}
}

func TestDrugInteraction_MultipleInteractionsInLongerList(t *testing.T) {
	// A four-drug list should surface the two pairs that collide
	// (warfarin+ibuprofen and metoprolol+verapamil) and skip the rest.
	out := invokeDrugInteraction(t,
		`{"drugs":["warfarin","ibuprofen","metoprolol","verapamil"]}`)
	if len(out.Interactions) != 2 {
		t.Fatalf("expected 2 interactions, got %d (%+v)", len(out.Interactions), out.Interactions)
	}
	pairs := map[string]bool{}
	for _, i := range out.Interactions {
		pairs[i.Pair[0]+"|"+i.Pair[1]] = true
	}
	if !pairs["ibuprofen|warfarin"] {
		t.Fatalf("missing warfarin+ibuprofen hit, got %+v", pairs)
	}
	if !pairs["metoprolol|verapamil"] {
		t.Fatalf("missing metoprolol+verapamil hit, got %+v", pairs)
	}
}
