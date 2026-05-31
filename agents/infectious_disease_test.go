package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// idConsultMsg builds an id_consult agent.Message for use in unit tests.
func idConsultMsg(patientID, caseID, site, severity string, allergies []string, culturePending bool) agent.Message {
	req := idConsult{
		PatientID:      patientID,
		CaseID:         caseID,
		Site:           site,
		Severity:       severity,
		Allergies:      allergies,
		CulturePending: culturePending,
	}
	body, _ := json.Marshal(req)
	return agent.NewMessage("user", "infectious_disease", agent.RoleUser, "id_consult",
		string(body), map[string]any{"case_id": caseID, "patient_id": patientID})
}

// decodeRec decodes the Content of an id_recommendation message.
func decodeRec(t *testing.T, msg agent.Message) idRecommendation {
	t.Helper()
	var rec idRecommendation
	if err := json.Unmarshal([]byte(msg.Content), &rec); err != nil {
		t.Fatalf("decode id_recommendation: %v\ncontent: %s", err, msg.Content)
	}
	return rec
}

// TestInfectiousDiseaseAgent_CAPMild checks mild CAP → azithromycin PO.
func TestInfectiousDiseaseAgent_CAPMild(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p1", "c1", "cap", "mild", nil, false)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if out[0].Type != "id_recommendation" {
		t.Errorf("want type=id_recommendation, got %q", out[0].Type)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "azithromycin") {
		t.Errorf("mild CAP: want azithromycin, got %q", rec.Antibiotic)
	}
	if rec.Route != "PO" {
		t.Errorf("mild CAP: want PO route, got %q", rec.Route)
	}
	if rec.DurationDays != 5 {
		t.Errorf("mild CAP: want 5 days, got %d", rec.DurationDays)
	}
}

// TestInfectiousDiseaseAgent_CAPMildMacrolideAllergy checks mild CAP with
// penicillin/macrolide allergy → doxycycline.
func TestInfectiousDiseaseAgent_CAPMildMacrolideAllergy(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p2", "c2", "cap", "mild", []string{"penicillin"}, false)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "doxycycline") {
		t.Errorf("mild CAP + pen allergy: want doxycycline, got %q", rec.Antibiotic)
	}
}

// TestInfectiousDiseaseAgent_CAPSevere checks severe CAP → pip-tazo + azithromycin IV.
func TestInfectiousDiseaseAgent_CAPSevere(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p3", "c3", "cap", "severe", nil, true)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "piperacillin") {
		t.Errorf("severe CAP: want piperacillin-tazobactam, got %q", rec.Antibiotic)
	}
	if rec.Route != "IV" {
		t.Errorf("severe CAP: want IV route, got %q", rec.Route)
	}
	// culture pending should inject de-escalation reminder
	if !strings.Contains(strings.ToLower(rec.DeEscalationCriteria), "culture") {
		t.Errorf("culture pending: de_escalation_criteria should mention culture, got %q", rec.DeEscalationCriteria)
	}
}

// TestInfectiousDiseaseAgent_CAPSeverePenAllergy checks severe CAP + pen allergy
// → aztreonam (no cross-reactivity).
func TestInfectiousDiseaseAgent_CAPSeverePenAllergy(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p4", "c4", "cap", "severe", []string{"penicillin", "amoxicillin"}, false)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "aztreonam") {
		t.Errorf("severe CAP + pen allergy: want aztreonam, got %q", rec.Antibiotic)
	}
}

// TestInfectiousDiseaseAgent_UTIUncomplicated checks uncomplicated UTI →
// nitrofurantoin PO 5d.
func TestInfectiousDiseaseAgent_UTIUncomplicated(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p5", "c5", "uti", "mild", nil, false)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "nitrofurantoin") {
		t.Errorf("uncomplicated UTI: want nitrofurantoin, got %q", rec.Antibiotic)
	}
	if rec.DurationDays != 5 {
		t.Errorf("uncomplicated UTI: want 5 days, got %d", rec.DurationDays)
	}
}

// TestInfectiousDiseaseAgent_UTIComplicated checks complicated UTI →
// ciprofloxacin 500mg BID x7d.
func TestInfectiousDiseaseAgent_UTIComplicated(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p6", "c6", "uti", "severe", nil, false)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "ciprofloxacin") {
		t.Errorf("complicated UTI: want ciprofloxacin, got %q", rec.Antibiotic)
	}
	if rec.DurationDays != 7 {
		t.Errorf("complicated UTI: want 7 days, got %d", rec.DurationDays)
	}
}

// TestInfectiousDiseaseAgent_CellulitisMild checks mild cellulitis → cephalexin QID x5d.
func TestInfectiousDiseaseAgent_CellulitisMild(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p7", "c7", "cellulitis", "mild", nil, false)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "cephalexin") {
		t.Errorf("mild cellulitis: want cephalexin, got %q", rec.Antibiotic)
	}
	if !strings.Contains(rec.Dose, "QID") {
		t.Errorf("mild cellulitis: want QID dosing, got %q", rec.Dose)
	}
}

// TestInfectiousDiseaseAgent_CellulitisMildPenAllergy checks mild cellulitis
// + penicillin allergy → clindamycin.
func TestInfectiousDiseaseAgent_CellulitisMildPenAllergy(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p8", "c8", "cellulitis", "mild", []string{"penicillin"}, false)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "clindamycin") {
		t.Errorf("mild cellulitis + pen allergy: want clindamycin, got %q", rec.Antibiotic)
	}
}

// TestInfectiousDiseaseAgent_Sepsis checks sepsis → vancomycin + pip-tazo IV.
func TestInfectiousDiseaseAgent_Sepsis(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p9", "c9", "sepsis", "severe", nil, true)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "vancomycin") {
		t.Errorf("sepsis: want vancomycin, got %q", rec.Antibiotic)
	}
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "piperacillin") {
		t.Errorf("sepsis: want piperacillin-tazobactam, got %q", rec.Antibiotic)
	}
	if rec.Route != "IV" {
		t.Errorf("sepsis: want IV route, got %q", rec.Route)
	}
}

// TestInfectiousDiseaseAgent_SepsisPenAllergy checks sepsis + pen allergy →
// vancomycin + aztreonam.
func TestInfectiousDiseaseAgent_SepsisPenAllergy(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p10", "c10", "sepsis", "severe", []string{"penicillin"}, false)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "aztreonam") {
		t.Errorf("sepsis + pen allergy: want aztreonam, got %q", rec.Antibiotic)
	}
}

// TestInfectiousDiseaseAgent_MRSA checks MRSA suspected → vancomycin IV.
func TestInfectiousDiseaseAgent_MRSA(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	req := idConsult{
		PatientID:         "p11",
		CaseID:            "c11",
		SuspectedOrganism: "MRSA",
		Site:              "mrsa",
		Severity:          "severe",
		CulturePending:    false,
	}
	body, _ := json.Marshal(req)
	msg := agent.NewMessage("user", "infectious_disease", agent.RoleUser, "id_consult",
		string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out[0])
	if !strings.Contains(strings.ToLower(rec.Antibiotic), "vancomycin") {
		t.Errorf("MRSA: want vancomycin, got %q", rec.Antibiotic)
	}
	if rec.DurationDays != 14 {
		t.Errorf("MRSA: want 14 days, got %d", rec.DurationDays)
	}
	if !strings.Contains(strings.ToLower(rec.IsolationPrecautions), "contact") {
		t.Errorf("MRSA: want contact precautions, got %q", rec.IsolationPrecautions)
	}
}

// TestInfectiousDiseaseAgent_CulturePendingDeEscalation verifies that any site
// with culture_pending=true always includes a de-escalation reminder referencing
// culture results.
func TestInfectiousDiseaseAgent_CulturePendingDeEscalation(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	sites := []string{"uti", "cellulitis", "sepsis"}
	for _, site := range sites {
		msg := idConsultMsg("p12", "c12", site, "mild", nil, true)
		out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
		if err != nil {
			t.Fatalf("%s HandleMessage: %v", site, err)
		}
		rec := decodeRec(t, out[0])
		if !strings.Contains(strings.ToLower(rec.DeEscalationCriteria), "culture") {
			t.Errorf("%s + culture pending: de_escalation_criteria must mention culture, got %q",
				site, rec.DeEscalationCriteria)
		}
	}
}

// TestInfectiousDiseaseAgent_MetadataPropagation verifies case_id and patient_id
// are set in the output metadata.
func TestInfectiousDiseaseAgent_MetadataPropagation(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := idConsultMsg("p99", "case-abc", "cap", "mild", nil, false)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if got := out[0].Metadata["patient_id"]; got != "p99" {
		t.Errorf("metadata patient_id: want p99, got %v", got)
	}
	if got := out[0].Metadata["case_id"]; got != "case-abc" {
		t.Errorf("metadata case_id: want case-abc, got %v", got)
	}
}

// TestInfectiousDiseaseAgent_InvalidJSON verifies that malformed Content returns
// a non-nil error and no output messages.
func TestInfectiousDiseaseAgent_InvalidJSON(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	msg := agent.NewMessage("user", "infectious_disease", agent.RoleUser, "id_consult",
		"not-json", map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Fatalf("want error for invalid JSON, got nil (out=%+v)", out)
	}
	if len(out) != 0 {
		t.Errorf("want no output on error, got %d", len(out))
	}
}

// TestInfectiousDiseaseAgent_AgentMeta checks ID, Name, and Capabilities.
func TestInfectiousDiseaseAgent_AgentMeta(t *testing.T) {
	a := &InfectiousDiseaseAgent{}
	if a.ID() != "infectious_disease" {
		t.Errorf("ID: want infectious_disease, got %q", a.ID())
	}
	if a.Name() != "Infectious Disease Specialist" {
		t.Errorf("Name: want 'Infectious Disease Specialist', got %q", a.Name())
	}
	caps := a.Capabilities()
	wantCaps := map[string]bool{"antibiotic_stewardship": false, "infection_management": false}
	for _, c := range caps {
		wantCaps[c] = true
	}
	for cap, found := range wantCaps {
		if !found {
			t.Errorf("Capabilities: missing %q", cap)
		}
	}
}
