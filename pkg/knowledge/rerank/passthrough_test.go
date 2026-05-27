package rerank

import (
	"context"
	"testing"
)

func TestPassthroughPreservesOrderAndScore(t *testing.T) {
	in := []Candidate{
		{ID: "a", Text: "alpha", Score: 0.91},
		{ID: "b", Text: "bravo", Score: 0.42},
		{ID: "c", Text: "charlie", Score: 0.08},
	}
	got, err := Passthrough{}.Rerank(context.Background(), "any query", in)
	if err != nil {
		t.Fatalf("Rerank returned error: %v", err)
	}
	if len(got) != len(in) {
		t.Fatalf("want %d results, got %d", len(in), len(got))
	}
	for i, c := range in {
		if got[i].ID != c.ID {
			t.Errorf("position %d: id want %q, got %q", i, c.ID, got[i].ID)
		}
		if got[i].Text != c.Text {
			t.Errorf("position %d: text want %q, got %q", i, c.Text, got[i].Text)
		}
		if got[i].Score != c.Score {
			t.Errorf("position %d: score want %v, got %v", i, c.Score, got[i].Score)
		}
		if got[i].RerankScore != c.Score {
			t.Errorf("position %d: RerankScore want %v (== Score), got %v", i, c.Score, got[i].RerankScore)
		}
	}
}

func TestPassthroughEmptyInput(t *testing.T) {
	got, err := Passthrough{}.Rerank(context.Background(), "q", nil)
	if err != nil {
		t.Fatalf("Rerank returned error on nil candidates: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty result, got %d entries", len(got))
	}
}
