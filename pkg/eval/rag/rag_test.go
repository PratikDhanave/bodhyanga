package rag

import (
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// Test fixtures: medical knowledge retrieval for community-acquired pneumonia
// ---------------------------------------------------------------------------

// sampleRetrieved returns a realistic top-3 retrieval list: two relevant
// clinical documents and one irrelevant result.
func sampleRetrieved() []Document {
	return []Document{
		{
			ID:      "abx-guidelines",
			Content: "Community-acquired pneumonia treatment includes empiric antibiotic therapy. First-line outpatient treatment is azithromycin 500mg on day 1 followed by 250mg on days 2-5, or doxycycline 100mg twice daily. For inpatient treatment, a beta-lactam plus a macrolide is recommended.",
			Score:   0.92,
		},
		{
			ID:      "dosing-info",
			Content: "Azithromycin dosing for CAP: loading dose 500mg orally on day 1, then 250mg daily for 4 days. Alternative: levofloxacin 750mg daily for 5 days. Duration of therapy is typically 5-7 days for uncomplicated cases. Monitor for QT prolongation with fluoroquinolones.",
			Score:   0.85,
		},
		{
			ID:      "unrelated-cardio",
			Content: "Atrial fibrillation management includes rate control with beta-blockers or calcium channel blockers. Anticoagulation with warfarin or direct oral anticoagulants reduces stroke risk. Catheter ablation may be considered for drug-refractory AF.",
			Score:   0.45,
		},
	}
}

// sampleRelevant returns the ground-truth relevant documents (the two
// pneumonia docs, not the cardiology one).
func sampleRelevant() []Document {
	return []Document{
		{ID: "abx-guidelines", Content: "Community-acquired pneumonia treatment includes empiric antibiotic therapy. First-line outpatient treatment is azithromycin 500mg on day 1 followed by 250mg on days 2-5, or doxycycline 100mg twice daily. For inpatient treatment, a beta-lactam plus a macrolide is recommended."},
		{ID: "dosing-info", Content: "Azithromycin dosing for CAP: loading dose 500mg orally on day 1, then 250mg daily for 4 days. Alternative: levofloxacin 750mg daily for 5 days. Duration of therapy is typically 5-7 days for uncomplicated cases. Monitor for QT prolongation with fluoroquinolones."},
	}
}

const sampleQuery = "What is the treatment for community-acquired pneumonia?"

const sampleAnswer = "CAP treatment includes azithromycin 500mg on day 1 followed by 250mg for 4 additional days. Doxycycline 100mg twice daily is an alternative. For inpatient cases, a beta-lactam plus macrolide combination is recommended. Duration of therapy is typically 5-7 days."

// ---------------------------------------------------------------------------
// RecallAtK
// ---------------------------------------------------------------------------

func TestRecallAtK_AllRelevantInTopK(t *testing.T) {
	got := RecallAtK(sampleRetrieved(), sampleRelevant(), 3)
	if got != 1.0 {
		t.Fatalf("RecallAtK(k=3): got %.4f, want 1.0", got)
	}
}

func TestRecallAtK_PartialRecall(t *testing.T) {
	got := RecallAtK(sampleRetrieved(), sampleRelevant(), 1)
	if got != 0.5 {
		t.Fatalf("RecallAtK(k=1): got %.4f, want 0.5", got)
	}
}

func TestRecallAtK_EmptyRelevant(t *testing.T) {
	got := RecallAtK(sampleRetrieved(), nil, 3)
	if got != 0 {
		t.Fatalf("RecallAtK(empty relevant): got %.4f, want 0", got)
	}
}

func TestRecallAtK_KExceedsRetrieved(t *testing.T) {
	got := RecallAtK(sampleRetrieved(), sampleRelevant(), 100)
	if got != 1.0 {
		t.Fatalf("RecallAtK(k=100): got %.4f, want 1.0", got)
	}
}

func TestRecallAtK_ZeroK(t *testing.T) {
	got := RecallAtK(sampleRetrieved(), sampleRelevant(), 0)
	if got != 0 {
		t.Fatalf("RecallAtK(k=0): got %.4f, want 0", got)
	}
}

func TestRecallAtK_NoOverlap(t *testing.T) {
	irrelevant := []Document{{ID: "missing-1"}, {ID: "missing-2"}}
	got := RecallAtK(sampleRetrieved(), irrelevant, 3)
	if got != 0 {
		t.Fatalf("RecallAtK(no overlap): got %.4f, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// PrecisionAtK
// ---------------------------------------------------------------------------

func TestPrecisionAtK_AllRelevant(t *testing.T) {
	got := PrecisionAtK(sampleRetrieved(), sampleRelevant(), 2)
	if got != 1.0 {
		t.Fatalf("PrecisionAtK(k=2): got %.4f, want 1.0", got)
	}
}

func TestPrecisionAtK_MixedResults(t *testing.T) {
	got := PrecisionAtK(sampleRetrieved(), sampleRelevant(), 3)
	want := 2.0 / 3.0
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("PrecisionAtK(k=3): got %.4f, want %.4f", got, want)
	}
}

func TestPrecisionAtK_EmptyRetrieved(t *testing.T) {
	got := PrecisionAtK(nil, sampleRelevant(), 3)
	if got != 0 {
		t.Fatalf("PrecisionAtK(empty retrieved): got %.4f, want 0", got)
	}
}

func TestPrecisionAtK_ZeroK(t *testing.T) {
	got := PrecisionAtK(sampleRetrieved(), sampleRelevant(), 0)
	if got != 0 {
		t.Fatalf("PrecisionAtK(k=0): got %.4f, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// MeanReciprocalRank
// ---------------------------------------------------------------------------

func TestMRR_FirstDocRelevant(t *testing.T) {
	got := MeanReciprocalRank(sampleRetrieved(), sampleRelevant())
	if got != 1.0 {
		t.Fatalf("MRR: got %.4f, want 1.0 (first doc is relevant)", got)
	}
}

func TestMRR_SecondDocRelevant(t *testing.T) {
	retrieved := []Document{
		{ID: "unrelated-cardio"},
		{ID: "abx-guidelines"},
		{ID: "dosing-info"},
	}
	got := MeanReciprocalRank(retrieved, sampleRelevant())
	if got != 0.5 {
		t.Fatalf("MRR: got %.4f, want 0.5 (first relevant at rank 2)", got)
	}
}

func TestMRR_NoRelevantFound(t *testing.T) {
	retrieved := []Document{{ID: "unrelated-1"}, {ID: "unrelated-2"}}
	got := MeanReciprocalRank(retrieved, sampleRelevant())
	if got != 0 {
		t.Fatalf("MRR: got %.4f, want 0 (no relevant doc)", got)
	}
}

func TestMRR_EmptyInputs(t *testing.T) {
	if got := MeanReciprocalRank(nil, sampleRelevant()); got != 0 {
		t.Fatalf("MRR(empty retrieved): got %.4f, want 0", got)
	}
	if got := MeanReciprocalRank(sampleRetrieved(), nil); got != 0 {
		t.Fatalf("MRR(empty relevant): got %.4f, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// NDCG
// ---------------------------------------------------------------------------

func TestNDCG_PerfectRanking(t *testing.T) {
	// Both relevant docs at top = perfect NDCG.
	got := NDCG(sampleRetrieved(), sampleRelevant(), 3)
	if got != 1.0 {
		t.Fatalf("NDCG(perfect): got %.4f, want 1.0", got)
	}
}

func TestNDCG_ImperfectRanking(t *testing.T) {
	// Put irrelevant doc first.
	retrieved := []Document{
		{ID: "unrelated-cardio"},
		{ID: "abx-guidelines"},
		{ID: "dosing-info"},
	}
	got := NDCG(retrieved, sampleRelevant(), 3)
	// DCG: 0/log2(2) + 1/log2(3) + 1/log2(4) = 0.6309 + 0.5 = 1.1309
	// IDCG: 1/log2(2) + 1/log2(3) = 1.0 + 0.6309 = 1.6309
	// NDCG = 1.1309 / 1.6309 ~= 0.6934
	want := (1.0/math.Log2(3) + 1.0/math.Log2(4)) / (1.0/math.Log2(2) + 1.0/math.Log2(3))
	if math.Abs(got-want) > 1e-4 {
		t.Fatalf("NDCG(imperfect): got %.4f, want %.4f", got, want)
	}
}

func TestNDCG_EmptyRelevant(t *testing.T) {
	got := NDCG(sampleRetrieved(), nil, 3)
	if got != 0 {
		t.Fatalf("NDCG(empty relevant): got %.4f, want 0", got)
	}
}

func TestNDCG_ZeroK(t *testing.T) {
	got := NDCG(sampleRetrieved(), sampleRelevant(), 0)
	if got != 0 {
		t.Fatalf("NDCG(k=0): got %.4f, want 0", got)
	}
}

func TestNDCG_KOne(t *testing.T) {
	// Only the first doc, which is relevant => NDCG = 1.0
	got := NDCG(sampleRetrieved(), sampleRelevant(), 1)
	if got != 1.0 {
		t.Fatalf("NDCG(k=1): got %.4f, want 1.0", got)
	}
}

// ---------------------------------------------------------------------------
// Faithfulness
// ---------------------------------------------------------------------------

func TestFaithfulness_AllSupported(t *testing.T) {
	got := Faithfulness(sampleAnswer, sampleRetrieved())
	// All sentences should be supported by the antibiotic docs.
	if got < 0.5 {
		t.Fatalf("Faithfulness: got %.4f, want >= 0.5 (most claims supported)", got)
	}
}

func TestFaithfulness_Hallucinated(t *testing.T) {
	hallucinated := "Pneumonia is treated with crystal healing and aromatherapy. Essential oils cure all bacterial infections."
	got := Faithfulness(hallucinated, sampleRetrieved())
	// Should score low since the claims don't match the retrieved docs.
	if got > 0.5 {
		t.Fatalf("Faithfulness(hallucinated): got %.4f, want < 0.5", got)
	}
}

func TestFaithfulness_EmptyAnswer(t *testing.T) {
	got := Faithfulness("", sampleRetrieved())
	if got != 0 {
		t.Fatalf("Faithfulness(empty): got %.4f, want 0", got)
	}
}

func TestFaithfulness_EmptyDocs(t *testing.T) {
	got := Faithfulness(sampleAnswer, nil)
	if got != 0 {
		t.Fatalf("Faithfulness(no docs): got %.4f, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Relevance
// ---------------------------------------------------------------------------

func TestRelevance_HighOverlap(t *testing.T) {
	got := Relevance(sampleQuery, sampleAnswer)
	// Query tokens (after stopword removal): "treatment", "community",
	// "acquired", "pneumonia". The answer uses the abbreviation "CAP" instead
	// of "community-acquired", so we expect ~0.25 (1 of 4 query tokens hit).
	if got < 0.2 {
		t.Fatalf("Relevance: got %.4f, want >= 0.2", got)
	}
}

func TestRelevance_NoOverlap(t *testing.T) {
	got := Relevance("quantum physics entanglement", "cats enjoy sleeping on warm surfaces")
	if got != 0 {
		t.Fatalf("Relevance(no overlap): got %.4f, want 0", got)
	}
}

func TestRelevance_EmptyQuery(t *testing.T) {
	got := Relevance("", sampleAnswer)
	if got != 0 {
		t.Fatalf("Relevance(empty query): got %.4f, want 0", got)
	}
}

func TestRelevance_IdenticalQueryAndAnswer(t *testing.T) {
	q := "pneumonia treatment guidelines"
	got := Relevance(q, q)
	if got != 1.0 {
		t.Fatalf("Relevance(identical): got %.4f, want 1.0", got)
	}
}

// ---------------------------------------------------------------------------
// Coverage
// ---------------------------------------------------------------------------

func TestCoverage_GoodCoverage(t *testing.T) {
	got := Coverage(sampleAnswer, sampleRelevant())
	// The answer mentions azithromycin, 500mg, doxycycline, beta-lactam, macrolide,
	// duration, days — many key terms from the relevant docs.
	if got < 0.2 {
		t.Fatalf("Coverage: got %.4f, want >= 0.2", got)
	}
}

func TestCoverage_NoCoverage(t *testing.T) {
	got := Coverage("This is completely unrelated to any medical topic.", sampleRelevant())
	if got > 0.1 {
		t.Fatalf("Coverage(unrelated): got %.4f, want <= 0.1", got)
	}
}

func TestCoverage_EmptyDocs(t *testing.T) {
	got := Coverage(sampleAnswer, nil)
	if got != 0 {
		t.Fatalf("Coverage(empty docs): got %.4f, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// EvaluateRAG (integration)
// ---------------------------------------------------------------------------

func TestEvaluateRAG_IntegrationMetrics(t *testing.T) {
	m := EvaluateRAG(sampleQuery, sampleAnswer, sampleRetrieved(), sampleRelevant(), 3)

	// Retrieval metrics: we know exact values from the unit tests above.
	if m.RecallAtK != 1.0 {
		t.Errorf("RecallAtK: got %.4f, want 1.0", m.RecallAtK)
	}
	wantPrecision := 2.0 / 3.0
	if math.Abs(m.PrecisionAtK-wantPrecision) > 1e-9 {
		t.Errorf("PrecisionAtK: got %.4f, want %.4f", m.PrecisionAtK, wantPrecision)
	}
	if m.MRR != 1.0 {
		t.Errorf("MRR: got %.4f, want 1.0", m.MRR)
	}
	if m.NDCG != 1.0 {
		t.Errorf("NDCG: got %.4f, want 1.0", m.NDCG)
	}

	// Generation metrics: bound checks (exact values depend on tokenization).
	if m.Faithfulness < 0.5 {
		t.Errorf("Faithfulness: got %.4f, want >= 0.5", m.Faithfulness)
	}
	if m.Relevance < 0.2 {
		t.Errorf("Relevance: got %.4f, want >= 0.2", m.Relevance)
	}
	if m.Coverage < 0.1 {
		t.Errorf("Coverage: got %.4f, want >= 0.1", m.Coverage)
	}
}

// ---------------------------------------------------------------------------
// EvaluateRAGBatch
// ---------------------------------------------------------------------------

func TestEvaluateRAGBatch_AveragesMetrics(t *testing.T) {
	results := []RetrievalResult{
		{
			Query:     sampleQuery,
			Retrieved: sampleRetrieved(),
			Relevant:  sampleRelevant(),
		},
		{
			Query: "diabetes management",
			Retrieved: []Document{
				{ID: "diabetes-guide", Content: "Type 2 diabetes management includes metformin as first-line therapy, lifestyle modifications, and blood glucose monitoring."},
				{ID: "unrelated", Content: "Geological formations in the Appalachian mountains."},
			},
			Relevant: []Document{
				{ID: "diabetes-guide", Content: "Type 2 diabetes management includes metformin as first-line therapy, lifestyle modifications, and blood glucose monitoring."},
			},
		},
	}
	answers := []string{
		sampleAnswer,
		"Diabetes management involves metformin as initial therapy along with diet and exercise modifications.",
	}

	m := EvaluateRAGBatch(results, answers, 3)

	// Both cases have recall@k = 1.0 so average should be 1.0.
	if m.RecallAtK != 1.0 {
		t.Errorf("Batch RecallAtK: got %.4f, want 1.0", m.RecallAtK)
	}
	// All generation metrics should be positive for these realistic examples.
	if m.Faithfulness <= 0 {
		t.Errorf("Batch Faithfulness: got %.4f, want > 0", m.Faithfulness)
	}
	if m.Relevance <= 0 {
		t.Errorf("Batch Relevance: got %.4f, want > 0", m.Relevance)
	}
}

func TestEvaluateRAGBatch_EmptyInput(t *testing.T) {
	m := EvaluateRAGBatch(nil, nil, 3)
	if m.RecallAtK != 0 || m.Faithfulness != 0 {
		t.Fatalf("expected zero metrics for empty input")
	}
}

func TestEvaluateRAGBatch_PanicsOnMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on mismatched lengths")
		}
	}()
	EvaluateRAGBatch(
		[]RetrievalResult{{Query: "q"}},
		[]string{"a1", "a2"},
		3,
	)
}

// ---------------------------------------------------------------------------
// Helper unit tests
// ---------------------------------------------------------------------------

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"Hello world.", 1},
		{"First. Second! Third?", 3},
		{"No punctuation", 1},
		{"", 0},
		{"One. Two.", 2},
	}
	for _, tc := range tests {
		got := splitSentences(tc.input)
		if len(got) != tc.want {
			t.Errorf("splitSentences(%q): got %d sentences %v, want %d", tc.input, len(got), got, tc.want)
		}
	}
}

func TestTokenize_DropsStopwords(t *testing.T) {
	tokens := tokenize("the treatment for community-acquired pneumonia is important")
	for _, tok := range tokens {
		if stopwords[tok] {
			t.Errorf("tokenize kept stopword %q", tok)
		}
	}
	// "treatment", "community", "acquired", "pneumonia", "important" should remain.
	if len(tokens) < 3 {
		t.Fatalf("too few tokens after stopword removal: %v", tokens)
	}
}

func TestTokenize_Lowercases(t *testing.T) {
	tokens := tokenize("Azithromycin CAP Treatment")
	for _, tok := range tokens {
		for _, r := range tok {
			if r >= 'A' && r <= 'Z' {
				t.Errorf("tokenize did not lowercase %q", tok)
			}
		}
	}
}
