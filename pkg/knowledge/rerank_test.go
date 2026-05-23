package knowledge

import (
	"context"
	"testing"
)

func TestLexicalReranker_PromotesRareTermMatches(t *testing.T) {
	// Corpus where "pulmonary" appears in exactly one document; "patient"
	// and "treatment" are common. The reranker should reward the candidate
	// whose snippet mentions the rare term.
	corpus := []Document{
		{ID: "1", Title: "Patient evaluation", Content: "patient treatment overview for the patient"},
		{ID: "2", Title: "Treatment basics", Content: "patient treatment basic patient care"},
		{ID: "3", Title: "Common workup", Content: "patient treatment patient treatment patient"},
		{ID: "4", Title: "Patient care", Content: "patient treatment patient care"},
		{ID: "5", Title: "Pulmonary embolism overview", Content: "pulmonary embolism diagnosis and pulmonary imaging"},
	}
	r := NewLexicalReranker(corpus)

	// Two candidates with identical original vector scores; rerank must
	// promote the one whose snippet contains the rare term "pulmonary".
	candidates := []Citation{
		{DocumentID: "common", Title: "Patient treatment", Snippet: "Patient treatment overview", Score: 0.5},
		{DocumentID: "rare", Title: "PE workup", Snippet: "Workup for pulmonary embolism with imaging", Score: 0.5},
	}
	got := r.Rerank(context.Background(), "pulmonary embolism", candidates, 2)
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d", len(got))
	}
	if got[0].DocumentID != "rare" {
		t.Fatalf("rare-term candidate should rank first; got order %s, %s", got[0].DocumentID, got[1].DocumentID)
	}
}

func TestLexicalReranker_RespectsTopK(t *testing.T) {
	corpus := []Document{
		{ID: "a", Title: "Alpha", Content: "alpha alpha alpha"},
		{ID: "b", Title: "Beta", Content: "beta beta beta"},
		{ID: "c", Title: "Gamma", Content: "gamma gamma"},
	}
	r := NewLexicalReranker(corpus)
	candidates := []Citation{
		{DocumentID: "1", Title: "alpha doc", Snippet: "alpha content here", Score: 0.7},
		{DocumentID: "2", Title: "beta doc", Snippet: "beta content here", Score: 0.6},
		{DocumentID: "3", Title: "gamma doc", Snippet: "gamma content here", Score: 0.5},
	}
	got := r.Rerank(context.Background(), "alpha beta gamma", candidates, 2)
	if len(got) != 2 {
		t.Fatalf("topK=2 must trim; got %d", len(got))
	}
	got = r.Rerank(context.Background(), "alpha", candidates, 10)
	if len(got) != 3 {
		t.Fatalf("topK>len should return all %d candidates, got %d", len(candidates), len(got))
	}
}

func TestLexicalReranker_EmptyCorpusDoesNotPanic(t *testing.T) {
	r := NewLexicalReranker(nil)
	candidates := []Citation{
		{DocumentID: "a", Title: "anything", Snippet: "snippet text", Score: 0.9},
		{DocumentID: "b", Title: "another", Snippet: "different text", Score: 0.4},
	}
	got := r.Rerank(context.Background(), "anything", candidates, 5)
	if len(got) != 2 {
		t.Fatalf("empty corpus must still return candidates; got %d", len(got))
	}
}

func TestLexicalReranker_EmptyCandidatesReturnsEmpty(t *testing.T) {
	r := NewLexicalReranker([]Document{{ID: "x", Title: "t", Content: "c"}})
	got := r.Rerank(context.Background(), "anything", nil, 5)
	if got == nil {
		t.Fatalf("Rerank must return a non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("empty candidates must yield empty result; got %d", len(got))
	}
	got = r.Rerank(context.Background(), "anything", []Citation{}, 5)
	if len(got) != 0 {
		t.Fatalf("empty candidates must yield empty result; got %d", len(got))
	}
}
