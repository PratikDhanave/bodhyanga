package hl7v2

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// TestParseAdmit asserts the A01 fixture round-trips into a medical.ADTEvent
// with the expected MR-typed patient identifier, admit type, facility,
// occurred-at sourced from EVN-2, and the working DG1 as primary diagnosis.
func TestParseAdmit(t *testing.T) {
	msg, err := ParseMessage(loadFixture(t, "adt_a01_admit.hl7"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.Header.MessageType != "ADT" || msg.Header.TriggerEvent != "A01" {
		t.Errorf("header MSH-9 unexpected: %+v", msg.Header)
	}
	if msg.Header.ControlID != "MSG00001" {
		t.Errorf("control id: %q", msg.Header.ControlID)
	}
	ev, err := MessageToADTEvent(msg)
	if err != nil {
		t.Fatalf("to ADTEvent: %v", err)
	}
	if ev.PatientID != "MRN12345" {
		t.Errorf("patient id: %q want MRN12345", ev.PatientID)
	}
	if ev.Type != medical.ADTAdmit {
		t.Errorf("type %q want admit", ev.Type)
	}
	if ev.Facility != "ICU" {
		t.Errorf("facility %q want ICU", ev.Facility)
	}
	if ev.Unit != "A" {
		t.Errorf("unit %q want A (bed component PV1-3.3)", ev.Unit)
	}
	if ev.PrimaryDx != "I50.9" {
		t.Errorf("primary dx %q want I50.9 (the W-priority DG1)", ev.PrimaryDx)
	}
	if ev.OccurredAt.IsZero() {
		t.Error("occurred_at zero — EVN-2 should have parsed")
	}
	// EVN-2 is "20250524103000" so OccurredAt should match.
	if got := ev.OccurredAt.Format("20060102150405"); got != "20250524103000" {
		t.Errorf("occurred_at = %s want 20250524103000", got)
	}
}

// TestParseDischarge asserts the A03 fixture maps to a discharge event, picks
// the W-priority DG1 over the F-priority follow-up diagnosis.
func TestParseDischarge(t *testing.T) {
	msg, err := ParseMessage(loadFixture(t, "adt_a03_discharge.hl7"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ev, err := MessageToADTEvent(msg)
	if err != nil {
		t.Fatalf("to ADTEvent: %v", err)
	}
	if ev.Type != medical.ADTDischarge {
		t.Errorf("type %q want discharge", ev.Type)
	}
	if ev.PrimaryDx != "I50.9" {
		t.Errorf("primary dx %q want I50.9 (working dx wins over secondary)", ev.PrimaryDx)
	}
}

// TestParseUpdate covers A08 (patient update) which maps to admit because it
// carries a refreshed encounter snapshot.
func TestParseUpdate(t *testing.T) {
	msg, err := ParseMessage(loadFixture(t, "adt_a08_update.hl7"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ev, err := MessageToADTEvent(msg)
	if err != nil {
		t.Fatalf("to ADTEvent: %v", err)
	}
	if ev.Type != medical.ADTAdmit {
		t.Errorf("type %q want admit (A08 update)", ev.Type)
	}
	if ev.PatientID != "MRN12345" {
		t.Errorf("patient id %q", ev.PatientID)
	}
	if ev.PrimaryDx != "" {
		t.Errorf("primary dx %q want empty (no DG1 in update)", ev.PrimaryDx)
	}
}

// TestParseMalformedReturnsError ensures a fixture without MSH errors out
// cleanly instead of panicking.
func TestParseMalformedReturnsError(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ParseMessage panicked on malformed input: %v", r)
		}
	}()
	_, err := ParseMessage(loadFixture(t, "adt_malformed.hl7"))
	if err == nil {
		t.Fatal("expected error on missing MSH, got nil")
	}
	// Empty body should also fail rather than crash.
	if _, err := ParseMessage(""); err == nil {
		t.Error("expected error on empty body")
	}
}

// TestCustomEncodingChars exercises the spec-required behaviour: delimiters
// are defined by MSH-1 and MSH-2 on each message, not hardcoded. Using # as
// the field delimiter should parse cleanly.
func TestCustomEncodingChars(t *testing.T) {
	// Field=#, Component=@, Repetition=`, Escape=\, Subcomponent=&
	raw := "MSH#@`\\&#ADTSYS#MERCY#BODHYANGA#MERCY#20250524103045##ADT@A01@ADT_A01#MSG-CUST#P#2.5\r" +
		"EVN#A01#20250524103000\r" +
		"PID#1##MRN999@@@MERCY@MR##DOE@JANE\r" +
		"PV1#1#I#ICU@9@A@MERCY\r"
	msg, err := ParseMessage(raw)
	if err != nil {
		t.Fatalf("parse custom delims: %v", err)
	}
	if msg.Encoding.Field != '#' || msg.Encoding.Component != '@' {
		t.Errorf("encoding not picked up: %+v", msg.Encoding)
	}
	ev, err := MessageToADTEvent(msg)
	if err != nil {
		t.Fatalf("to ADTEvent: %v", err)
	}
	if ev.PatientID != "MRN999" {
		t.Errorf("patient id %q (custom delims) want MRN999", ev.PatientID)
	}
	if ev.Facility != "ICU" {
		t.Errorf("facility %q (custom delims) want ICU", ev.Facility)
	}
}

// TestBuildACK_AAEchoesControlIDAndFlipsRoutes ensures ACK has MSA|AA|<ctrl>
// and that sender/receiver are flipped relative to the inbound MSH.
func TestBuildACK_AAEchoesControlIDAndFlipsRoutes(t *testing.T) {
	msg, err := ParseMessage(loadFixture(t, "adt_a01_admit.hl7"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ack := BuildACK(msg.Header, "AA", "Accepted")
	if !strings.Contains(ack, "MSA|AA|MSG00001|Accepted") {
		t.Errorf("ACK missing expected MSA: %s", ack)
	}
	// Round-trip the ACK back through ParseMessage: the resulting MSH-3 (sender)
	// must be the original MSH-5 (receiver) i.e. "BODHYANGA".
	parsed, err := ParseMessage(ack)
	if err != nil {
		t.Fatalf("ACK doesn't round-trip parse: %v", err)
	}
	if parsed.Header.SendingApp != "BODHYANGA" {
		t.Errorf("ACK sender = %q want BODHYANGA (flipped from inbound receiver)", parsed.Header.SendingApp)
	}
	if parsed.Header.ReceivingApp != "ADTSYS" {
		t.Errorf("ACK receiver = %q want ADTSYS (flipped from inbound sender)", parsed.Header.ReceivingApp)
	}
}

// TestBuildACK_AECarriesErrorText is the negative path: a parser/business
// failure should emit an AE ACK whose MSA-3 carries the diagnostic.
func TestBuildACK_AECarriesErrorText(t *testing.T) {
	hdr := MSH{
		SendingApp:        "PARTNER",
		SendingFacility:   "F1",
		ReceivingApp:      "BODHYANGA",
		ReceivingFacility: "F2",
		ControlID:         "CID-7",
		TriggerEvent:      "A03",
		VersionID:         "2.5",
		ProcessingID:      "P",
	}
	ack := BuildACK(hdr, "AE", "missing PID")
	if !strings.Contains(ack, "MSA|AE|CID-7|missing PID") {
		t.Errorf("AE ACK missing diagnostic text: %s", ack)
	}
}

// TestUnsupportedTriggerEvent confirms the parser rejects an ADT trigger we
// haven't implemented support for (e.g. A05 pre-admit).
func TestUnsupportedTriggerEvent(t *testing.T) {
	raw := "MSH|^~\\&|ADTSYS|MERCY|BODHYANGA|MERCY|20250524103045||ADT^A05|MSG00010|P|2.5\r" +
		"PID|1||MRN1^^^MERCY^MR\r"
	msg, err := ParseMessage(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := MessageToADTEvent(msg); err == nil {
		t.Error("expected unsupported-trigger error for A05")
	}
}

// TestPIDFallback_NonMR ensures the parser falls back to the first PID-3
// repetition when no MR-typed identifier is present.
func TestPIDFallback_NonMR(t *testing.T) {
	raw := "MSH|^~\\&|S|F|R|F|20250524103045||ADT^A01|C1|P|2.5\r" +
		"EVN|A01|20250524103000\r" +
		"PID|1||PI-7^^^FAC^PI\r" +
		"PV1|1|I|ICU\r"
	msg, err := ParseMessage(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ev, err := MessageToADTEvent(msg)
	if err != nil {
		t.Fatalf("to ADTEvent: %v", err)
	}
	if ev.PatientID != "PI-7" {
		t.Errorf("expected fallback PI identifier PI-7, got %q", ev.PatientID)
	}
}

// TestParseHL7Time_Formats spot-checks several timestamp shapes the v2 spec
// allows.
func TestParseHL7Time_Formats(t *testing.T) {
	cases := []struct {
		in       string
		wantYear int
		wantErr  bool
	}{
		{"", 1, false}, // empty -> zero time, no error
		{"20250524103045", 2025, false},
		{"20250524", 2025, false},
		{"202505241030", 2025, false},
		{"20250524103045+0530", 2025, false},
		{"not-a-date", 0, true},
	}
	for _, c := range cases {
		got, err := parseHL7Time(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%q: err=%v wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && !got.IsZero() && got.Year() != c.wantYear {
			t.Errorf("%q: year %d want %d", c.in, got.Year(), c.wantYear)
		}
	}
}
