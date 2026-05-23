package agents

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

func sampleProposal() medical.DiagnosisProposal {
	return medical.DiagnosisProposal{
		Diagnosis:    "PE",
		Confidence:   0.78,
		Differential: []string{"PE", "CAP", "Bronchitis"},
		Rationale:    "Pleuritic chest pain, sudden onset, hypoxia.",
	}
}

func sampleRankerState() medical.CaseState {
	return medical.CaseState{
		CaseID:       "ranker-1",
		Presentation: "Sudden pleuritic chest pain and hypoxia.",
		Condition:    "pe",
	}
}

func TestDifferentialRanker_HappyPath(t *testing.T) {
	mock := &llm.Mock{
		Resp: &llm.Response{Content: `["CAP", "Bronchitis", "PE"]`},
	}
	r := &DifferentialRanker{Client: mock}

	got, err := r.Rerank(context.Background(), sampleRankerState(), sampleProposal())
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	want := []string{"CAP", "Bronchitis", "PE"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Rerank: got %v want %v", got, want)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mock.Calls))
	}
	call := mock.Calls[0]
	if !call.CacheSystem {
		t.Fatalf("expected CacheSystem=true on system prompt")
	}
	if call.JSONSchema == nil || call.JSONSchema.Name != "rerank_differential" {
		t.Fatalf("expected rerank_differential schema, got %+v", call.JSONSchema)
	}
}

func TestDifferentialRanker_LLMErrorReturnsOriginal(t *testing.T) {
	mock := &llm.Mock{Err: errors.New("boom")}
	r := &DifferentialRanker{Client: mock}

	original := sampleProposal()
	got, err := r.Rerank(context.Background(), sampleRankerState(), original)
	if err != nil {
		t.Fatalf("Rerank should swallow LLM error, got %v", err)
	}
	if !reflect.DeepEqual(got, original.Differential) {
		t.Fatalf("Rerank should return original on LLM error; got %v want %v", got, original.Differential)
	}
}

func TestDifferentialRanker_BadJSONReturnsOriginal(t *testing.T) {
	mock := &llm.Mock{Resp: &llm.Response{Content: "this is not json at all"}}
	r := &DifferentialRanker{Client: mock}

	original := sampleProposal()
	got, err := r.Rerank(context.Background(), sampleRankerState(), original)
	if err != nil {
		t.Fatalf("Rerank should swallow parse error, got %v", err)
	}
	if !reflect.DeepEqual(got, original.Differential) {
		t.Fatalf("Rerank should return original on bad JSON; got %v want %v", got, original.Differential)
	}
}

func TestDifferentialRanker_EmptyArrayReturnsOriginal(t *testing.T) {
	mock := &llm.Mock{Resp: &llm.Response{Content: `[]`}}
	r := &DifferentialRanker{Client: mock}

	original := sampleProposal()
	got, err := r.Rerank(context.Background(), sampleRankerState(), original)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if !reflect.DeepEqual(got, original.Differential) {
		t.Fatalf("empty array should yield original; got %v want %v", got, original.Differential)
	}
}

func TestDifferentialRanker_PreservesCaseInsensitiveEntries(t *testing.T) {
	// The LLM emits the entries with different capitalisation than the
	// caller supplied. The ranker must preserve the LLM-emitted form
	// (so downstream display shows what the model said), and treat
	// "CAP" and "Cap" as the distinct strings they are.
	mock := &llm.Mock{
		Resp: &llm.Response{Content: `["Cap", "bronchitis", "Pulmonary Embolism"]`},
	}
	r := &DifferentialRanker{Client: mock}

	state := sampleRankerState()
	p := medical.DiagnosisProposal{
		Diagnosis:    "PE",
		Differential: []string{"PE", "CAP", "Bronchitis"},
	}
	got, err := r.Rerank(context.Background(), state, p)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	want := []string{"Cap", "bronchitis", "Pulmonary Embolism"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected case-preserving rerank; got %v want %v", got, want)
	}
	// Proposal must not be mutated by the ranker.
	if !reflect.DeepEqual(p.Differential, []string{"PE", "CAP", "Bronchitis"}) {
		t.Fatalf("Rerank mutated input proposal; got %v", p.Differential)
	}
}
