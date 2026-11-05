package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
)

// radiologyRequestMsg builds a well-formed agent.Message for testing the
// RadiologistAgent.HandleMessage path end-to-end.
func radiologyRequestMsg(patientID, caseID, modality, bodyPart, reportText string) agent.Message {
	body, _ := json.Marshal(radiologyRequest{
		PatientID:  patientID,
		CaseID:     caseID,
		Modality:   modality,
		BodyPart:   bodyPart,
		ReportText: reportText,
	})
	return agent.NewMessage("test_planner", "radiologist", agent.RoleAgent,
		"radiology_request", string(body),
		map[string]any{"case_id": caseID, "patient_id": patientID})
}

// decodeReport is a test helper that parses the radiology_report payload.
func decodeReport(t *testing.T, content string) radiologyReport {
	t.Helper()
	var r radiologyReport
	if err := json.Unmarshal([]byte(content), &r); err != nil {
		t.Fatalf("decodeReport: %v (content=%q)", err, content)
	}
	return r
}

// --------------------------------------------------------------------------
// HandleMessage integration tests
// --------------------------------------------------------------------------

func TestRadiologistHandleMessage_Routing(t *testing.T) {
	a := &RadiologistAgent{}
	msg := radiologyRequestMsg("p1", "c1", "CXR", "", "Normal chest X-ray.")
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output message, got %d", len(out))
	}
	if out[0].Type != "radiology_report" {
		t.Errorf("want Type=radiology_report, got %q", out[0].Type)
	}
	// Reply should be addressed back to the caller.
	if out[0].To != "test_planner" {
		t.Errorf("want To=test_planner, got %q", out[0].To)
	}
	if out[0].From != "radiologist" {
		t.Errorf("want From=radiologist, got %q", out[0].From)
	}
}

func TestRadiologistHandleMessage_MetadataPropagation(t *testing.T) {
	a := &RadiologistAgent{}
	msg := radiologyRequestMsg("p42", "case-99", "CXR", "", "Pneumothorax noted.")
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 message, got %d", len(out))
	}
	if got := out[0].Metadata["patient_id"]; got != "p42" {
		t.Errorf("patient_id: want p42, got %v", got)
	}
	if got := out[0].Metadata["case_id"]; got != "case-99" {
		t.Errorf("case_id: want case-99, got %v", got)
	}
}

func TestRadiologistHandleMessage_InvalidJSON(t *testing.T) {
	a := &RadiologistAgent{}
	msg := agent.NewMessage("src", "radiologist", agent.RoleAgent,
		"radiology_request", "{not valid json}", nil)
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// --------------------------------------------------------------------------
// CXR rule tests
// --------------------------------------------------------------------------

func TestRadiologist_CXR_Pneumothorax_Emergent(t *testing.T) {
	req := radiologyRequest{Modality: "CXR", ReportText: "Large left-sided pneumothorax identified.", BodyPart: ""}
	r := interpretRadiologyReport(req)
	if r.Urgency != "emergent" {
		t.Errorf("urgency: want emergent, got %q", r.Urgency)
	}
	if !r.CriticalFinding {
		t.Error("critical_finding should be true for pneumothorax")
	}
	if !r.FollowUpNeeded {
		t.Error("follow_up_needed should be true for emergent")
	}
	assertFindingPresent(t, r.Findings, "Pneumothorax")
}

func TestRadiologist_CXR_PleuralEffusionLarge_Urgent(t *testing.T) {
	req := radiologyRequest{Modality: "CXR", ReportText: "Pleural effusion large on the right side.", BodyPart: ""}
	r := interpretRadiologyReport(req)
	if r.Urgency != "urgent" {
		t.Errorf("urgency: want urgent, got %q", r.Urgency)
	}
	if r.CriticalFinding {
		t.Error("critical_finding should be false for urgent-only finding")
	}
	if !r.FollowUpNeeded {
		t.Error("follow_up_needed should be true for urgent")
	}
	assertFindingPresent(t, r.Findings, "Large pleural effusion")
}

func TestRadiologist_CXR_Infiltrate_Routine(t *testing.T) {
	req := radiologyRequest{Modality: "CXR", ReportText: "Patchy infiltrate in the right lower lobe.", BodyPart: ""}
	r := interpretRadiologyReport(req)
	if r.Urgency != "routine" {
		t.Errorf("urgency: want routine, got %q", r.Urgency)
	}
	if r.CriticalFinding {
		t.Error("critical_finding should be false for routine")
	}
	assertFindingPresent(t, r.Findings, "Pulmonary infiltrate")
}

// Pneumothorax (emergent) + infiltrate (routine) in same report → emergent wins.
func TestRadiologist_CXR_MultipleFindings_HighestUrgencyWins(t *testing.T) {
	req := radiologyRequest{Modality: "CXR", ReportText: "Pneumothorax with adjacent infiltrate.", BodyPart: ""}
	r := interpretRadiologyReport(req)
	if r.Urgency != "emergent" {
		t.Errorf("urgency: want emergent, got %q", r.Urgency)
	}
	if !r.CriticalFinding {
		t.Error("critical_finding should be true")
	}
}

// --------------------------------------------------------------------------
// CT chest rule tests
// --------------------------------------------------------------------------

func TestRadiologist_CTChest_PulmonaryEmbolism_Emergent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "chest", ReportText: "Saddle pulmonary embolism at the bifurcation."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "emergent" {
		t.Errorf("urgency: want emergent, got %q", r.Urgency)
	}
	if !r.CriticalFinding {
		t.Error("critical_finding should be true for PE")
	}
	assertFindingPresent(t, r.Findings, "Pulmonary embolism")
}

func TestRadiologist_CTChest_AorticDissection_Emergent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "chest", ReportText: "Type A aortic dissection extending to the arch."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "emergent" {
		t.Errorf("urgency: want emergent, got %q", r.Urgency)
	}
	if !r.CriticalFinding {
		t.Error("critical_finding should be true for aortic dissection")
	}
}

func TestRadiologist_CTChest_Pneumonia_Urgent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "chest", ReportText: "Consolidation consistent with pneumonia in the left lower lobe."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "urgent" {
		t.Errorf("urgency: want urgent, got %q", r.Urgency)
	}
	if r.CriticalFinding {
		t.Error("critical_finding should be false for pneumonia")
	}
}

// --------------------------------------------------------------------------
// CT head rule tests
// --------------------------------------------------------------------------

func TestRadiologist_CTHead_Hemorrhage_Emergent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "head", ReportText: "Subarachnoid hemorrhage in the basal cisterns."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "emergent" {
		t.Errorf("urgency: want emergent, got %q", r.Urgency)
	}
	if !r.CriticalFinding {
		t.Error("critical_finding should be true for hemorrhage")
	}
}

func TestRadiologist_CTHead_Bleed_Emergent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "brain", ReportText: "Acute subdural bleed with mass effect."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "emergent" {
		t.Errorf("urgency: want emergent, got %q", r.Urgency)
	}
	if !r.CriticalFinding {
		t.Error("critical_finding should be true for bleed")
	}
}

func TestRadiologist_CTHead_Stroke_Emergent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "head", ReportText: "Early changes consistent with ischemic stroke in MCA territory."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "emergent" {
		t.Errorf("urgency: want emergent, got %q", r.Urgency)
	}
	if !r.CriticalFinding {
		t.Error("critical_finding should be true for stroke")
	}
}

func TestRadiologist_CTHead_Infarct_Emergent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "head", ReportText: "Subacute infarct in the left parietal lobe."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "emergent" {
		t.Errorf("urgency: want emergent, got %q", r.Urgency)
	}
}

func TestRadiologist_CTHead_Mass_Urgent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "head", ReportText: "Ring-enhancing mass in the right temporal lobe, likely neoplasm."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "urgent" {
		t.Errorf("urgency: want urgent, got %q", r.Urgency)
	}
	if r.CriticalFinding {
		t.Error("critical_finding should be false for urgent-only mass")
	}
	assertFindingPresent(t, r.Findings, "Intracranial mass")
}

// --------------------------------------------------------------------------
// Abdomen rule tests
// --------------------------------------------------------------------------

func TestRadiologist_Abdomen_FreeAir_Emergent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "abdomen", ReportText: "Free air under the diaphragm suggesting bowel perforation."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "emergent" {
		t.Errorf("urgency: want emergent, got %q", r.Urgency)
	}
	if !r.CriticalFinding {
		t.Error("critical_finding should be true for free air")
	}
	assertFindingPresent(t, r.Findings, "Free intraperitoneal air")
}

func TestRadiologist_Abdomen_Obstruction_Urgent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "abdominal", ReportText: "Small bowel obstruction with transition point in the right iliac fossa."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "urgent" {
		t.Errorf("urgency: want urgent, got %q", r.Urgency)
	}
	if r.CriticalFinding {
		t.Error("critical_finding should be false for obstruction")
	}
}

func TestRadiologist_Abdomen_Appendicitis_Urgent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "pelvis", ReportText: "Appendix measures 11mm with periappendiceal fat stranding consistent with appendicitis."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "urgent" {
		t.Errorf("urgency: want urgent, got %q", r.Urgency)
	}
	assertFindingPresent(t, r.Findings, "Appendicitis")
}

// --------------------------------------------------------------------------
// No findings (normal study)
// --------------------------------------------------------------------------

func TestRadiologist_NoFindings_Routine(t *testing.T) {
	req := radiologyRequest{Modality: "CXR", ReportText: "Normal chest X-ray. Cardiac silhouette within normal limits.", BodyPart: ""}
	r := interpretRadiologyReport(req)
	if r.Urgency != "routine" {
		t.Errorf("urgency: want routine, got %q", r.Urgency)
	}
	if r.CriticalFinding {
		t.Error("critical_finding should be false for normal study")
	}
	if r.FollowUpNeeded {
		t.Error("follow_up_needed should be false for normal study")
	}
	if len(r.Findings) != 1 || r.Findings[0] != "No acute findings identified" {
		t.Errorf("findings: want [\"No acute findings identified\"], got %v", r.Findings)
	}
}

// UnknownModality/bodyPart combination should still return a well-formed report.
func TestRadiologist_UnknownModality_NoFindings(t *testing.T) {
	req := radiologyRequest{Modality: "MRI", BodyPart: "knee", ReportText: "Anterior cruciate ligament tear."}
	r := interpretRadiologyReport(req)
	if r.Urgency != "routine" {
		t.Errorf("urgency: want routine, got %q", r.Urgency)
	}
	if r.CriticalFinding {
		t.Error("critical_finding should be false")
	}
}

// --------------------------------------------------------------------------
// Impression format tests
// --------------------------------------------------------------------------

func TestRadiologist_ImpressionFormat_Emergent(t *testing.T) {
	req := radiologyRequest{Modality: "CXR", ReportText: "Tension pneumothorax.", BodyPart: ""}
	r := interpretRadiologyReport(req)
	if len(r.Impression) == 0 {
		t.Fatal("impression should not be empty")
	}
	if !hasPrefix(r.Impression, "EMERGENT:") {
		t.Errorf("impression should start with EMERGENT: — got %q", r.Impression)
	}
}

func TestRadiologist_ImpressionFormat_Urgent(t *testing.T) {
	req := radiologyRequest{Modality: "CT", BodyPart: "chest", ReportText: "Bilateral pneumonia."}
	r := interpretRadiologyReport(req)
	if !hasPrefix(r.Impression, "URGENT:") {
		t.Errorf("impression should start with URGENT: — got %q", r.Impression)
	}
}

func TestRadiologist_ImpressionFormat_Normal(t *testing.T) {
	req := radiologyRequest{Modality: "CXR", ReportText: "Clear lungs bilaterally.", BodyPart: ""}
	r := interpretRadiologyReport(req)
	if r.Impression == "" {
		t.Fatal("impression should not be empty")
	}
	// Normal study should have a bland follow-up note, no prefix.
	if hasPrefix(r.Impression, "EMERGENT:") || hasPrefix(r.Impression, "URGENT:") {
		t.Errorf("normal study impression should not have urgency prefix: %q", r.Impression)
	}
}

// --------------------------------------------------------------------------
// Agent identity
// --------------------------------------------------------------------------

func TestRadiologistAgent_Identity(t *testing.T) {
	a := &RadiologistAgent{}
	if a.ID() != "radiologist" {
		t.Errorf("ID: want radiologist, got %q", a.ID())
	}
	if a.Name() != "Radiology Specialist" {
		t.Errorf("Name: want Radiology Specialist, got %q", a.Name())
	}
	caps := a.Capabilities()
	wantCaps := map[string]bool{"imaging_interpretation": true, "radiology_report": true}
	for _, c := range caps {
		wantCaps[c] = false
	}
	for cap, unmatched := range wantCaps {
		if unmatched {
			t.Errorf("missing capability: %q", cap)
		}
	}
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func assertFindingPresent(t *testing.T, findings []string, want string) {
	t.Helper()
	for _, f := range findings {
		if f == want {
			return
		}
	}
	t.Errorf("expected finding %q in %v", want, findings)
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
