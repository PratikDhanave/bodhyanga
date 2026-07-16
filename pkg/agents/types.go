// Package agents holds the four MARA slice agents as MAF workflow executors:
// intake → test_planner → diagnostician → care_plan, with the diagnostician
// looping back to the planner until confident (architecture.md §1, §6).
package agents

import (
	"crypto/sha256"
	"encoding/hex"
)

// PatientCase is the raw intake payload. CaseID is PHI-bearing: nothing
// downstream may log, audit, or emit it raw — use HashCaseID.
type PatientCase struct {
	CaseID   string   `json:"case_id"`
	Symptoms []string `json:"symptoms"`
	History  []string `json:"history"`
}

// DiagnosticQuery asks the test planner for a (further) workup. Round 0 is
// the initial symptom-only pass; the diagnostician re-emits it with an
// incremented round when unconfident.
type DiagnosticQuery struct {
	Case       PatientCase
	Round      int
	PriorTests []TestResult
}

// TestResult is one completed test.
type TestResult struct {
	Name  string
	Value string
}

// Workup is a query plus the results of the tests the planner ordered.
type Workup struct {
	Query   DiagnosticQuery
	Results []TestResult
}

// DiagnosisResult is the diagnostician's terminal message for a case.
type DiagnosisResult struct {
	CaseID     string
	Diagnosis  string
	Confidence float64
	Rounds     int
	// Forced is true when the max-round guard fired before the confidence
	// bar was met (architecture.md §6 termination guarantee).
	Forced   bool
	Evidence []string
	// Citations are the retrieved guideline snippets that grounded the
	// diagnosis (specs/003 RAG). Empty when RAG is disabled or retrieval
	// found nothing. Provenance only — never patient data.
	Citations []Citation `json:"citations,omitempty"`
}

// Citation is a retrieved knowledge snippet with provenance and a relevance
// score. It is a slice-local mirror of knowledge.Citation /
// medical.DiagnosisCitation, kept here deliberately so the lean agents types
// do not import the whole pkg/medical (care-plan generators, ADT, registry)
// just for a 4-field struct (plan §import-vs-adapt).
type Citation struct {
	Source  string  `json:"source"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

// CarePlan is the workflow output.
type CarePlan struct {
	CaseID    string
	Diagnosis string
	Forced    bool
	Steps     []string
}

// HashCaseID follows bodh's PHI rule: identifiers on events, spans, and audit
// records are always sha256(caseID)[:8] hex, never the raw value.
func HashCaseID(caseID string) string {
	sum := sha256.Sum256([]byte(caseID))
	return hex.EncodeToString(sum[:])[:8]
}
