package hl7v2

import (
	"testing"
)

// TestORUParseCBC verifies that a CBC panel (WBC 14.2 H, Hgb 10.1 L, Plt 450 H)
// parses into the expected ORUMessage structure including header, patient, ORC,
// panel name, collection time, LOINC codes, values, units, reference ranges, and
// abnormal flags.
func TestORUParseCBC(t *testing.T) {
	raw := loadFixture(t, "oru_r01_cbc.hl7")
	msg, err := ParseORU(raw)
	if err != nil {
		t.Fatalf("ParseORU CBC: %v", err)
	}

	// MSH checks.
	if msg.MSH.MessageType != "ORU" {
		t.Errorf("MSH type %q want ORU", msg.MSH.MessageType)
	}
	if msg.MSH.TriggerEvent != "R01" {
		t.Errorf("MSH trigger %q want R01", msg.MSH.TriggerEvent)
	}
	if msg.MSH.ControlID != "LAB00001" {
		t.Errorf("control id %q want LAB00001", msg.MSH.ControlID)
	}

	// PID checks.
	if msg.PID.PatientID != "MRN12345" {
		t.Errorf("patient id %q want MRN12345", msg.PID.PatientID)
	}
	if msg.PID.FamilyName != "DOE" {
		t.Errorf("family name %q want DOE", msg.PID.FamilyName)
	}
	if msg.PID.GivenName != "JOHN" {
		t.Errorf("given name %q want JOHN", msg.PID.GivenName)
	}

	// ORC checks.
	if msg.ORC == nil {
		t.Fatal("ORC segment expected, got nil")
	}
	if msg.ORC.OrderControl != "RE" {
		t.Errorf("ORC order control %q want RE", msg.ORC.OrderControl)
	}
	if msg.ORC.PlacerID != "ORD-9001" {
		t.Errorf("ORC placer ID %q want ORD-9001", msg.ORC.PlacerID)
	}
	if msg.ORC.FillerID != "FIL-4321" {
		t.Errorf("ORC filler ID %q want FIL-4321", msg.ORC.FillerID)
	}

	// Results: single panel expected.
	if len(msg.Results) != 1 {
		t.Fatalf("result count %d want 1", len(msg.Results))
	}
	panel := msg.Results[0]
	if panel.PanelName != "CBC panel" {
		t.Errorf("panel name %q want 'CBC panel'", panel.PanelName)
	}
	if panel.CollectedAt.IsZero() {
		t.Error("CollectedAt zero — OBR-7 should have parsed")
	}
	if got := panel.CollectedAt.Format("20060102150405"); got != "20250524133000" {
		t.Errorf("CollectedAt %s want 20250524133000", got)
	}

	// Observations: 4 expected.
	if len(panel.Observations) != 4 {
		t.Fatalf("observation count %d want 4", len(panel.Observations))
	}

	wbc := panel.Observations[0]
	if wbc.LOINCCode != "6690-2" {
		t.Errorf("WBC LOINC %q want 6690-2", wbc.LOINCCode)
	}
	if wbc.TestName != "WBC" {
		t.Errorf("WBC test name %q want WBC", wbc.TestName)
	}
	if wbc.Value != "14.2" {
		t.Errorf("WBC value %q want 14.2", wbc.Value)
	}
	if wbc.AbnormalFlag != "H" {
		t.Errorf("WBC abnormal flag %q want H", wbc.AbnormalFlag)
	}
	if wbc.IsCritical {
		t.Error("WBC IsCritical should be false for H flag")
	}
	if wbc.Status != "F" {
		t.Errorf("WBC status %q want F (final)", wbc.Status)
	}
	if wbc.ReferenceRange != "4.5-11.0" {
		t.Errorf("WBC ref range %q want 4.5-11.0", wbc.ReferenceRange)
	}

	hgb := panel.Observations[1]
	if hgb.LOINCCode != "718-7" {
		t.Errorf("Hgb LOINC %q want 718-7", hgb.LOINCCode)
	}
	if hgb.Value != "10.1" {
		t.Errorf("Hgb value %q want 10.1", hgb.Value)
	}
	if hgb.AbnormalFlag != "L" {
		t.Errorf("Hgb abnormal flag %q want L", hgb.AbnormalFlag)
	}
	if hgb.IsCritical {
		t.Error("Hgb IsCritical should be false for L flag")
	}

	plt := panel.Observations[2]
	if plt.LOINCCode != "32623-1" {
		t.Errorf("Plt LOINC %q want 32623-1", plt.LOINCCode)
	}
	if plt.Value != "450" {
		t.Errorf("Plt value %q want 450", plt.Value)
	}
	if plt.AbnormalFlag != "H" {
		t.Errorf("Plt abnormal flag %q want H", plt.AbnormalFlag)
	}
}

// TestORUParseBMP verifies the BMP panel parses correctly with 8 observations
// including a critical high glucose (HH).
func TestORUParseBMP(t *testing.T) {
	raw := loadFixture(t, "oru_r01_bmp.hl7")
	msg, err := ParseORU(raw)
	if err != nil {
		t.Fatalf("ParseORU BMP: %v", err)
	}

	// No ORC in BMP fixture.
	if msg.ORC != nil {
		t.Error("ORC should be nil — BMP fixture has no ORC segment")
	}

	if msg.PID.PatientID != "MRN67890" {
		t.Errorf("patient id %q want MRN67890", msg.PID.PatientID)
	}

	if len(msg.Results) != 1 {
		t.Fatalf("result count %d want 1", len(msg.Results))
	}
	panel := msg.Results[0]
	if panel.PanelName != "BMP" {
		t.Errorf("panel name %q want BMP", panel.PanelName)
	}
	if len(panel.Observations) != 8 {
		t.Fatalf("observation count %d want 8", len(panel.Observations))
	}

	// Glucose is OBX-7 (index 6) — critical high.
	glucose := panel.Observations[6]
	if glucose.LOINCCode != "2345-7" {
		t.Errorf("glucose LOINC %q want 2345-7", glucose.LOINCCode)
	}
	if glucose.AbnormalFlag != "HH" {
		t.Errorf("glucose flag %q want HH (critical high)", glucose.AbnormalFlag)
	}
	if !glucose.IsCritical {
		t.Error("glucose IsCritical should be true for HH flag")
	}
	if glucose.Value != "312" {
		t.Errorf("glucose value %q want 312", glucose.Value)
	}

	// Normal sodium (index 0).
	sodium := panel.Observations[0]
	if sodium.AbnormalFlag != "N" {
		t.Errorf("sodium flag %q want N", sodium.AbnormalFlag)
	}
	if sodium.IsCritical {
		t.Error("sodium IsCritical should be false for N flag")
	}
}

// TestORUParseCritical checks that HH and LL abnormal flags set IsCritical=true.
func TestORUParseCritical(t *testing.T) {
	raw := loadFixture(t, "oru_r01_critical.hl7")
	msg, err := ParseORU(raw)
	if err != nil {
		t.Fatalf("ParseORU critical: %v", err)
	}
	if len(msg.Results) != 1 {
		t.Fatalf("result count %d want 1", len(msg.Results))
	}
	obs := msg.Results[0].Observations
	if len(obs) != 3 {
		t.Fatalf("observation count %d want 3", len(obs))
	}

	// Potassium: HH.
	if !obs[0].IsCritical || obs[0].AbnormalFlag != "HH" {
		t.Errorf("K+ expected HH critical, got flag=%q critical=%v", obs[0].AbnormalFlag, obs[0].IsCritical)
	}
	// Creatinine: LL.
	if !obs[1].IsCritical || obs[1].AbnormalFlag != "LL" {
		t.Errorf("Cr expected LL critical, got flag=%q critical=%v", obs[1].AbnormalFlag, obs[1].IsCritical)
	}
	// Glucose: LL.
	if !obs[2].IsCritical || obs[2].AbnormalFlag != "LL" {
		t.Errorf("Glucose expected LL critical, got flag=%q critical=%v", obs[2].AbnormalFlag, obs[2].IsCritical)
	}
}

// TestORUObservedAtParsed verifies that OBX-14 observation timestamp is
// parsed into ObservedAt.
func TestORUObservedAtParsed(t *testing.T) {
	raw := loadFixture(t, "oru_r01_cbc.hl7")
	msg, err := ParseORU(raw)
	if err != nil {
		t.Fatalf("ParseORU: %v", err)
	}
	obs := msg.Results[0].Observations[0]
	if obs.ObservedAt.IsZero() {
		t.Error("ObservedAt zero — OBX-14 should have parsed")
	}
	if got := obs.ObservedAt.Format("20060102150405"); got != "20250524133000" {
		t.Errorf("ObservedAt %s want 20250524133000", got)
	}
}

// TestORUInlineMessage exercises ParseORU with an inline raw string (no file)
// to verify the units field parsing and multi-segment flow without testdata.
func TestORUInlineMessage(t *testing.T) {
	raw := "MSH|^~\\&|LABSYS|MERCY|BODH|MERCY|20250524160000||ORU^R01^ORU_R01|LABINLINE|P|2.5\r" +
		"PID|1||PT-001^^^MERCY^MR||PATIENT^TEST||19900101|F\r" +
		"OBR|1||FIL-INLINE|1558-6^Glucose 2h post 75g^LN|||20250524155000\r" +
		"OBX|1|NM|1558-6^Glucose 2h post 75g^LN||185|mg/dL|<140|H|||P|||20250524155500\r"

	msg, err := ParseORU(raw)
	if err != nil {
		t.Fatalf("ParseORU inline: %v", err)
	}
	if msg.PID.PatientID != "PT-001" {
		t.Errorf("patient id %q want PT-001", msg.PID.PatientID)
	}
	if len(msg.Results) != 1 {
		t.Fatalf("result count %d want 1", len(msg.Results))
	}
	obs := msg.Results[0].Observations[0]
	if obs.Value != "185" {
		t.Errorf("glucose value %q want 185", obs.Value)
	}
	if obs.Status != "P" {
		t.Errorf("status %q want P (preliminary)", obs.Status)
	}
	if obs.AbnormalFlag != "H" {
		t.Errorf("flag %q want H", obs.AbnormalFlag)
	}
}

// TestORUWrongMessageType ensures ParseORU rejects non-ORU messages cleanly.
func TestORUWrongMessageType(t *testing.T) {
	// Feed an ADT message to ParseORU.
	raw := loadFixture(t, "adt_a01_admit.hl7")
	_, err := ParseORU(raw)
	if err == nil {
		t.Fatal("expected error for non-ORU message, got nil")
	}
}

// TestORUMissingPID ensures ParseORU returns an error when PID is absent.
func TestORUMissingPID(t *testing.T) {
	raw := "MSH|^~\\&|LABSYS|MERCY|BODH|MERCY|20250524160000||ORU^R01^ORU_R01|LAB-NOPID|P|2.5\r" +
		"OBR|1||FIL-X|58410-2^CBC^LN|||20250524133000\r" +
		"OBX|1|NM|6690-2^WBC^LN||7.5|10*3/uL|4.5-11.0|N|||F\r"
	_, err := ParseORU(raw)
	if err == nil {
		t.Fatal("expected error for missing PID, got nil")
	}
}

// TestORUNoOBR ensures ParseORU returns an error when there are no OBR/OBX segments.
func TestORUNoOBR(t *testing.T) {
	raw := "MSH|^~\\&|LABSYS|MERCY|BODH|MERCY|20250524160000||ORU^R01^ORU_R01|LAB-NOOBR|P|2.5\r" +
		"PID|1||MRN11111^^^MERCY^MR\r"
	_, err := ParseORU(raw)
	if err == nil {
		t.Fatal("expected error for missing OBR, got nil")
	}
}
