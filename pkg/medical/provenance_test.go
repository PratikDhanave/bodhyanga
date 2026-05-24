package medical

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHashPrompt_DeterministicAndDifferentInputsDiffer(t *testing.T) {
	a := HashPrompt("system A", "user A")
	b := HashPrompt("system A", "user A")
	if a != b {
		t.Fatalf("HashPrompt not deterministic: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("expected 64-char sha256 hex, got %d (%q)", len(a), a)
	}

	c := HashPrompt("system A", "user B")
	if a == c {
		t.Fatalf("HashPrompt collided on different user inputs")
	}
	d := HashPrompt("system B", "user A")
	if a == d {
		t.Fatalf("HashPrompt collided on different system inputs")
	}

	// Boundary check: the delimiter prevents naive concatenation collisions
	// where ("ab", "c") and ("a", "bc") would hash identically.
	e := HashPrompt("ab", "c")
	f := HashPrompt("a", "bc")
	if e == f {
		t.Fatalf("HashPrompt allows boundary collision between (ab,c) and (a,bc)")
	}
}

func TestMakeProvenance_FieldsPopulated(t *testing.T) {
	citations := []DiagnosisCitation{
		{Source: "IDSA 2019 §III", Title: "CAP", Snippet: "infiltrate plus signs of infection"},
		{Source: "USPSTF 2024", Title: "Cough Evaluation", Snippet: "..."},
	}
	p := MakeProvenance("anthropic", "claude-sonnet-4-6", "system prompt", "user content", citations, "diagnostician", "llm")

	if p.Provider != "anthropic" {
		t.Errorf("Provider=%q", p.Provider)
	}
	if p.Model != "claude-sonnet-4-6" {
		t.Errorf("Model=%q", p.Model)
	}
	if p.AgentID != "diagnostician" {
		t.Errorf("AgentID=%q", p.AgentID)
	}
	if p.Variant != "llm" {
		t.Errorf("Variant=%q", p.Variant)
	}
	if got, want := p.PromptHash, HashPrompt("system prompt", "user content"); got != want {
		t.Errorf("PromptHash=%q, want %q", got, want)
	}
	if len(p.CitationIDs) != 2 {
		t.Fatalf("CitationIDs len=%d", len(p.CitationIDs))
	}
	if p.CitationIDs[0] != "IDSA 2019 §III" || p.CitationIDs[1] != "USPSTF 2024" {
		t.Errorf("CitationIDs=%v", p.CitationIDs)
	}
	if p.Timestamp == "" {
		t.Errorf("Timestamp empty")
	}
	if _, err := time.Parse(time.RFC3339, p.Timestamp); err != nil {
		t.Errorf("Timestamp %q not RFC3339: %v", p.Timestamp, err)
	}
}

func TestMakeProvenance_OmitsEmptyCitations(t *testing.T) {
	p := MakeProvenance("mock", "mock-model", "sys", "usr", nil, "diagnostician", "llm")
	if p.CitationIDs != nil {
		t.Fatalf("CitationIDs should be nil when no citations supplied, got %v", p.CitationIDs)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "citation_ids") {
		t.Fatalf("citation_ids should be omitted from JSON, got %s", b)
	}

	// Citations with only empty Source values should still omit the field.
	p2 := MakeProvenance("mock", "mock-model", "sys", "usr",
		[]DiagnosisCitation{{Source: "", Title: "untitled"}}, "diagnostician", "llm")
	if p2.CitationIDs != nil {
		t.Fatalf("empty-source citations should leave CitationIDs nil, got %v", p2.CitationIDs)
	}
}

func TestDiagnosisProposal_ProvenanceOmitemptyInJSON(t *testing.T) {
	// Rule-based output omits Provenance; the JSON should NOT carry the key.
	p := DiagnosisProposal{Diagnosis: "X", Confidence: 0.5, Rationale: "r"}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "provenance") {
		t.Fatalf("provenance should be omitted from rule-based JSON, got %s", b)
	}
}
