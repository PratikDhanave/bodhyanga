package hl7v2

import (
	"strings"
	"testing"
)

// ---- ParseMessage error branches ----

func TestParseMessage_Empty(t *testing.T) {
	_, err := ParseMessage("")
	if err == nil {
		t.Error("expected error for empty message")
	}
}

func TestParseMessage_NoMSH(t *testing.T) {
	_, err := ParseMessage("PID|something|here")
	if err == nil {
		t.Error("expected error for non-MSH start")
	}
}

func TestParseMessage_TooShort(t *testing.T) {
	_, err := ParseMessage("MSH|^~")
	if err == nil {
		t.Error("expected error for MSH too short")
	}
}

func TestParseMessage_MalformedSegment(t *testing.T) {
	// Second segment has a type that is not 3 chars.
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01^ADT_A01|CTRL001|P|2.5\rAB|data"
	_, err := ParseMessage(raw)
	if err == nil {
		t.Error("expected error for malformed segment type")
	}
}

// ---- parseMSH errors ----

func TestParseMSH_MissingMessageType(t *testing.T) {
	// MSH without MSH-9 (message type).
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||||||CTRL001|P|2.5"
	_, err := ParseMessage(raw)
	if err == nil {
		t.Error("expected error for missing MSH-9")
	}
}

func TestParseMSH_MissingControlID(t *testing.T) {
	// MSH with message type but no control ID (MSH-10).
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01"
	_, err := ParseMessage(raw)
	if err == nil {
		t.Error("expected error for missing MSH-10")
	}
}

// ---- repetition function (0%) ----

func TestRepetition(t *testing.T) {
	enc := DefaultEncoding()

	// Empty field.
	got := repetition("", enc, 1)
	if got != "" {
		t.Errorf("repetition empty = %q, want empty", got)
	}

	// n < 1.
	got = repetition("a~b", enc, 0)
	if got != "" {
		t.Errorf("repetition n=0 = %q, want empty", got)
	}

	// Valid repetitions.
	got = repetition("first~second~third", enc, 1)
	if got != "first" {
		t.Errorf("repetition 1 = %q, want first", got)
	}
	got = repetition("first~second~third", enc, 2)
	if got != "second" {
		t.Errorf("repetition 2 = %q, want second", got)
	}

	// Out of range.
	got = repetition("first~second", enc, 5)
	if got != "" {
		t.Errorf("repetition out of range = %q, want empty", got)
	}
}

// ---- repetitions function (line 257) ----

func TestRepetitions_Empty(t *testing.T) {
	enc := DefaultEncoding()
	got := repetitions("", enc)
	if got != nil {
		t.Errorf("repetitions empty = %v, want nil", got)
	}
}

// ---- triggerToType unknown (line 427) ----

func TestTriggerToType_Unknown(t *testing.T) {
	_, err := triggerToType("A99")
	if err == nil {
		t.Error("expected error for unknown trigger")
	}
}

func TestTriggerToType_A02Transfer(t *testing.T) {
	evType, err := triggerToType("A02")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evType != "transfer" {
		t.Errorf("expected transfer, got %q", evType)
	}
}

func TestTriggerToType_A08(t *testing.T) {
	evType, err := triggerToType("A08")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evType != "admit" {
		t.Errorf("expected admit for A08, got %q", evType)
	}
}

// ---- MessageToADTEvent error branches ----

func TestMessageToADTEvent_NilMessage(t *testing.T) {
	_, err := MessageToADTEvent(nil)
	if err == nil {
		t.Error("expected error for nil message")
	}
}

func TestMessageToADTEvent_NotADT(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ORU^R01|CTRL001|P|2.5\rPID|||12345^^^HOSP^MR"
	msg, err := ParseMessage(raw)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	_, err = MessageToADTEvent(msg)
	if err == nil {
		t.Error("expected error for non-ADT message")
	}
}

func TestMessageToADTEvent_UnsupportedTrigger(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A99|CTRL001|P|2.5\rPID|||12345^^^HOSP^MR"
	msg, _ := ParseMessage(raw)
	_, err := MessageToADTEvent(msg)
	if err == nil {
		t.Error("expected error for unsupported trigger")
	}
}

func TestMessageToADTEvent_NoPID(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5"
	msg, _ := ParseMessage(raw)
	_, err := MessageToADTEvent(msg)
	if err == nil {
		t.Error("expected error for missing PID")
	}
}

func TestMessageToADTEvent_EmptyPatientID(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\rPID|||"
	msg, _ := ParseMessage(raw)
	_, err := MessageToADTEvent(msg)
	if err == nil {
		t.Error("expected error for empty PID-3")
	}
}

// ---- pickPatientID branches ----

func TestPickPatientID_NoMRType(t *testing.T) {
	enc := DefaultEncoding()
	seg := Segment{
		Type:   "PID",
		Fields: []string{"PID", "", "", "12345^^^HOSP^PI"},
	}
	id := pickPatientID(seg, enc)
	// Should fall back to first CX-1.
	if id != "12345" {
		t.Errorf("expected 12345, got %q", id)
	}
}

func TestPickPatientID_MRType(t *testing.T) {
	enc := DefaultEncoding()
	seg := Segment{
		Type:   "PID",
		Fields: []string{"PID", "", "", "99999^^^HOSP^PI~12345^^^HOSP^MR"},
	}
	id := pickPatientID(seg, enc)
	if id != "12345" {
		t.Errorf("expected 12345 (MR), got %q", id)
	}
}

func TestPickPatientID_EmptyField(t *testing.T) {
	enc := DefaultEncoding()
	seg := Segment{
		Type:   "PID",
		Fields: []string{"PID", "", ""},
	}
	id := pickPatientID(seg, enc)
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}
}

// ---- primaryDiagnosis branches ----

func TestPrimaryDiagnosis_WorkingDx(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\r" +
		"PID|||12345^^^HOSP^MR\r" +
		"DG1|1||I50.9^Heart failure|||A\r" +
		"DG1|2||J18.9^Pneumonia|||W"
	msg, _ := ParseMessage(raw)
	dx := primaryDiagnosis(msg)
	if dx != "J18.9" {
		t.Errorf("expected J18.9 (working diagnosis), got %q", dx)
	}
}

func TestPrimaryDiagnosis_NoDG1(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\rPID|||12345^^^HOSP^MR"
	msg, _ := ParseMessage(raw)
	dx := primaryDiagnosis(msg)
	if dx != "" {
		t.Errorf("expected empty, got %q", dx)
	}
}

// ---- BuildACK branches ----

func TestBuildACK_EmptyTrigger(t *testing.T) {
	ack := BuildACK(MSH{ControlID: "CTRL1"}, "AA", "OK")
	if !strings.Contains(ack, "ACK") {
		t.Error("expected ACK in output")
	}
	// Empty trigger should default to A01.
	if !strings.Contains(ack, "A01") {
		t.Error("expected A01 default trigger")
	}
}

func TestBuildACK_EmptyControlID(t *testing.T) {
	ack := BuildACK(MSH{}, "AE", "Error text")
	if !strings.Contains(ack, "ACK-") {
		t.Error("expected ACK- prefix for empty control ID")
	}
}

func TestBuildACK_EmptyVersion(t *testing.T) {
	ack := BuildACK(MSH{ControlID: "X"}, "AA", "ok")
	if !strings.Contains(ack, "2.5") {
		t.Error("expected default version 2.5")
	}
}

func TestBuildACK_EmptyProcessingID(t *testing.T) {
	ack := BuildACK(MSH{ControlID: "X"}, "AA", "ok")
	if !strings.Contains(ack, "|P|") {
		t.Error("expected default processing ID P")
	}
}

func TestBuildACK_WithAllFields(t *testing.T) {
	msh := MSH{
		SendingApp:        "APP1",
		SendingFacility:   "FAC1",
		ReceivingApp:      "APP2",
		ReceivingFacility: "FAC2",
		TriggerEvent:      "A03",
		ControlID:         "CTRL123",
		VersionID:         "2.6",
		ProcessingID:      "D",
	}
	ack := BuildACK(msh, "AR", "Rejected")
	if !strings.Contains(ack, "A03") {
		t.Error("expected trigger A03")
	}
	if !strings.Contains(ack, "CTRL123") {
		t.Error("expected control ID")
	}
}

// ---- ORU ParseORU error branches ----

func TestParseORU_NotORU(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\rPID|||12345"
	_, err := ParseORU(raw)
	if err == nil {
		t.Error("expected error for non-ORU message")
	}
}

func TestParseORU_WrongTrigger(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ORU^R02|CTRL001|P|2.5\rPID|||12345"
	_, err := ParseORU(raw)
	if err == nil {
		t.Error("expected error for wrong trigger")
	}
}

func TestParseORU_NoPID(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ORU^R01|CTRL001|P|2.5"
	_, err := ParseORU(raw)
	if err == nil {
		t.Error("expected error for missing PID")
	}
}

func TestParseORU_NoOBR(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ORU^R01|CTRL001|P|2.5\rPID|||12345^^^HOSP^MR"
	_, err := ParseORU(raw)
	if err == nil {
		t.Error("expected error for no OBR/OBX")
	}
}

func TestParseORU_OBXWithoutOBR(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ORU^R01|CTRL001|P|2.5\r" +
		"PID|||12345^^^HOSP^MR\r" +
		"OBX|1|NM|2951-2^Sodium||140|mmol/L|136-145|N||F"
	msg, err := ParseORU(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msg.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(msg.Results))
	}
	if msg.Results[0].PanelName != "UNKNOWN" {
		t.Errorf("expected UNKNOWN panel, got %q", msg.Results[0].PanelName)
	}
}

func TestParseORU_CriticalFlag(t *testing.T) {
	raw := "MSH|^~\\&|LIS|FAC|RECV|RFAC|20250101120000||ORU^R01|CTRL001|P|2.5\r" +
		"PID|||12345^^^HOSP^MR\r" +
		"OBR|1||F001|24326-1^CBC||||||||||||||||||F\r" +
		"OBX|1|NM|2951-2^Sodium||120|mmol/L|136-145|LL||F"
	msg, err := ParseORU(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !msg.Results[0].Observations[0].IsCritical {
		t.Error("expected IsCritical=true for LL flag")
	}
}

func TestParseORU_OBRCodeFallback(t *testing.T) {
	// OBR-4 with code only (no display name).
	raw := "MSH|^~\\&|LIS|FAC|RECV|RFAC|20250101120000||ORU^R01|CTRL001|P|2.5\r" +
		"PID|||12345^^^HOSP^MR\r" +
		"OBR|1||F001|24326-1\r" +
		"OBX|1|NM|2951-2^Sodium||140|mmol/L"
	msg, err := ParseORU(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Results[0].PanelName != "24326-1" {
		t.Errorf("expected code fallback, got %q", msg.Results[0].PanelName)
	}
}

// ---- OBX unit display text preference ----

func TestParseOBXSegment_UnitDisplayText(t *testing.T) {
	enc := DefaultEncoding()
	seg := Segment{
		Type:   "OBX",
		Fields: []string{"OBX", "1", "NM", "2951-2^Sodium", "", "140", "mmol/L^millimoles per liter", "136-145", "N", "", "", "F", "", "", "20250101120000"},
	}
	obs := parseOBXSegment(seg, enc)
	// Should prefer display text (component 2).
	if obs.Units != "millimoles per liter" {
		t.Errorf("expected display text units, got %q", obs.Units)
	}
}

// ---- ORM ParseORM error branches ----

func TestParseORM_NotORM(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\rPID|||12345"
	_, err := ParseORM(raw)
	if err == nil {
		t.Error("expected error for non-ORM message")
	}
}

func TestParseORM_WrongTrigger(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ORM^O02|CTRL001|P|2.5\rPID|||12345"
	_, err := ParseORM(raw)
	if err == nil {
		t.Error("expected error for wrong trigger")
	}
}

func TestParseORM_NoPID(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ORM^O01|CTRL001|P|2.5"
	_, err := ParseORM(raw)
	if err == nil {
		t.Error("expected error for missing PID")
	}
}

func TestParseORM_NoORC(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ORM^O01|CTRL001|P|2.5\r" +
		"PID|||12345^^^HOSP^MR"
	_, err := ParseORM(raw)
	if err == nil {
		t.Error("expected error for missing ORC")
	}
}

func TestParseORM_NoOBR(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ORM^O01|CTRL001|P|2.5\r" +
		"PID|||12345^^^HOSP^MR\r" +
		"ORC|NW|CASE1"
	_, err := ParseORM(raw)
	if err == nil {
		t.Error("expected error for missing OBR")
	}
}

// ---- ORM round-trip ----

func TestORM_RoundTrip(t *testing.T) {
	built := BuildORM("PT-123", "CASE-456", "58410-2", "CBC panel", "S")
	encoded := built.Encode()

	parsed, err := ParseORM(encoded)
	if err != nil {
		t.Fatalf("ParseORM: %v", err)
	}

	if parsed.PID.PatientID != "PT-123" {
		t.Errorf("PatientID = %q, want PT-123", parsed.PID.PatientID)
	}
	if parsed.ORC.OrderControl != "NW" {
		t.Errorf("OrderControl = %q, want NW", parsed.ORC.OrderControl)
	}
	if parsed.OBR.ServiceID != "58410-2" {
		t.Errorf("ServiceID = %q, want 58410-2", parsed.OBR.ServiceID)
	}
	if parsed.OBR.Priority != "S" {
		t.Errorf("Priority = %q, want S", parsed.OBR.Priority)
	}
}

func TestORM_DefaultPriority(t *testing.T) {
	built := BuildORM("PT-123", "CASE-456", "58410-2", "CBC", "")
	if built.OBR.Priority != "R" {
		t.Errorf("default priority = %q, want R", built.OBR.Priority)
	}
}

// ---- component helper edge cases ----

func TestComponent_Empty(t *testing.T) {
	enc := DefaultEncoding()
	if component("", enc, 1) != "" {
		t.Error("expected empty for empty field")
	}
	if component("a^b", enc, 0) != "" {
		t.Error("expected empty for n=0")
	}
	if component("a^b", enc, 5) != "" {
		t.Error("expected empty for out of range")
	}
}

// ---- parseHL7Time edge cases ----

func TestParseHL7Time_WithTimezone(t *testing.T) {
	ts, err := parseHL7Time("20250101120000+0500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.IsZero() {
		t.Error("expected non-zero time")
	}
}

func TestParseHL7Time_ShortFormats(t *testing.T) {
	_, err := parseHL7Time("202501")
	if err != nil {
		t.Fatalf("YYYYMM: %v", err)
	}
	_, err = parseHL7Time("2025")
	if err != nil {
		t.Fatalf("YYYY: %v", err)
	}
}

func TestParseHL7Time_Unparseable(t *testing.T) {
	_, err := parseHL7Time("not-a-date")
	if err == nil {
		t.Error("expected error for unparseable timestamp")
	}
}

// ---- MessageToADTEvent with EVN segment ----

func TestMessageToADTEvent_WithEVN(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\r" +
		"EVN|A01|20250115140000\r" +
		"PID|||12345^^^HOSP^MR\r" +
		"PV1||I|ICU^^BED1"
	msg, err := ParseMessage(raw)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	event, err := MessageToADTEvent(msg)
	if err != nil {
		t.Fatalf("MessageToADTEvent: %v", err)
	}
	// Should use EVN-2 timestamp, not MSH-7.
	if event.OccurredAt.Day() != 15 {
		t.Errorf("expected day 15 from EVN-2, got %d", event.OccurredAt.Day())
	}
	if event.Facility != "ICU" {
		t.Errorf("facility = %q, want ICU", event.Facility)
	}
	if event.Unit != "BED1" {
		t.Errorf("unit = %q, want BED1", event.Unit)
	}
}

// ---- Newline normalization ----

func TestParseMessage_WindowsNewlines(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\r\nPID|||12345^^^HOSP^MR"
	msg, err := ParseMessage(raw)
	if err != nil {
		t.Fatalf("ParseMessage with \\r\\n: %v", err)
	}
	if len(msg.Segments) != 2 {
		t.Errorf("expected 2 segments, got %d", len(msg.Segments))
	}
}

func TestParseMessage_UnixNewlines(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\nPID|||12345^^^HOSP^MR"
	msg, err := ParseMessage(raw)
	if err != nil {
		t.Fatalf("ParseMessage with \\n: %v", err)
	}
	if len(msg.Segments) != 2 {
		t.Errorf("expected 2 segments, got %d", len(msg.Segments))
	}
}

// ---- ParseMessage with trailing \r ----

func TestParseMessage_TrailingCR(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\r\r"
	msg, err := ParseMessage(raw)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if len(msg.Segments) != 1 {
		t.Errorf("expected 1 segment, got %d", len(msg.Segments))
	}
}

// ---- parseSegment MSH too short ----

func TestParseSegment_MSHTooShort(t *testing.T) {
	enc := DefaultEncoding()
	_, err := parseSegment("MSH", enc, true)
	if err == nil {
		t.Error("expected error for MSH too short")
	}
}

// ---- primaryDiagnosis fallback (no working dx) ----

func TestPrimaryDiagnosis_FallbackFirst(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\r" +
		"PID|||12345^^^HOSP^MR\r" +
		"DG1|1||I50.9^Heart failure|||A\r" +
		"DG1|2||E11.9^T2DM|||A"
	msg, _ := ParseMessage(raw)
	dx := primaryDiagnosis(msg)
	// No working dx, should return first DG1's code.
	if dx != "I50.9" {
		t.Errorf("expected I50.9, got %q", dx)
	}
}

// ---- MessageToADTEvent without PV1 ----

func TestMessageToADTEvent_NoPV1(t *testing.T) {
	raw := "MSH|^~\\&|APP|FAC|RECV|RFAC|20250101120000||ADT^A01|CTRL001|P|2.5\r" +
		"PID|||12345^^^HOSP^MR"
	msg, _ := ParseMessage(raw)
	event, err := MessageToADTEvent(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Facility != "" {
		t.Errorf("expected empty facility without PV1, got %q", event.Facility)
	}
}

// ---- parseHL7Time YYYYMMDD ----

func TestParseHL7Time_YYYYMMDD(t *testing.T) {
	ts, err := parseHL7Time("20250115")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Day() != 15 {
		t.Errorf("expected day 15, got %d", ts.Day())
	}
}

// ---- parseHL7Time YYYYMMDDHH ----

func TestParseHL7Time_YYYYMMDDHH(t *testing.T) {
	ts, err := parseHL7Time("2025011514")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Hour() != 14 {
		t.Errorf("expected hour 14, got %d", ts.Hour())
	}
}

// ---- parseHL7Time YYYYMMDDHHMM ----

func TestParseHL7Time_YYYYMMDDHHMM(t *testing.T) {
	ts, err := parseHL7Time("202501151430")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Minute() != 30 {
		t.Errorf("expected minute 30, got %d", ts.Minute())
	}
}

// ---- ORM Encode with empty timestamp ----

func TestORMEncode_EmptyTimestamp(t *testing.T) {
	orm := ORMMessage{
		MSH: MSHSegment{
			SendingApp:   "APP",
			ReceivingApp: "EHR",
			MessageType:  "ORM",
			TriggerEvent: "O01",
			ControlID:    "CTRL1",
			ProcessingID: "P",
			VersionID:    "2.5",
			// Timestamp intentionally empty.
		},
		PID: PIDSegment{PatientID: "PT1"},
		ORC: ORCSegment{OrderControl: "NW"},
		OBR: OBRSegment{ServiceID: "58410-2", ServiceName: "CBC"},
	}
	encoded := orm.Encode()
	if !strings.Contains(encoded, "ORM") {
		t.Error("expected ORM in encoded output")
	}
}
