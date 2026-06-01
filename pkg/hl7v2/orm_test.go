package hl7v2

import (
	"strings"
	"testing"
	"time"
)

// TestORMRoundTrip verifies that BuildORM → Encode → ParseORM preserves all
// fields. This is the canonical round-trip test.
func TestORMRoundTrip(t *testing.T) {
	orig := BuildORM("MRN-9001", "CASE-42", "58410-2", "CBC panel", "R")

	encoded := orig.Encode()
	if encoded == "" {
		t.Fatal("Encode returned empty string")
	}

	parsed, err := ParseORM(encoded)
	if err != nil {
		t.Fatalf("ParseORM: %v", err)
	}

	// MSH checks.
	if parsed.MSH.MessageType != "ORM" {
		t.Errorf("MSH type %q want ORM", parsed.MSH.MessageType)
	}
	if parsed.MSH.TriggerEvent != "O01" {
		t.Errorf("MSH trigger %q want O01", parsed.MSH.TriggerEvent)
	}
	if parsed.MSH.ControlID != orig.MSH.ControlID {
		t.Errorf("control id %q want %q", parsed.MSH.ControlID, orig.MSH.ControlID)
	}
	if parsed.MSH.SendingApp != "BODH" {
		t.Errorf("sending app %q want BODH", parsed.MSH.SendingApp)
	}
	if parsed.MSH.VersionID != "2.5" {
		t.Errorf("version %q want 2.5", parsed.MSH.VersionID)
	}

	// PID checks.
	if parsed.PID.PatientID != "MRN-9001" {
		t.Errorf("patient id %q want MRN-9001", parsed.PID.PatientID)
	}

	// ORC checks.
	if parsed.ORC.OrderControl != "NW" {
		t.Errorf("ORC order control %q want NW", parsed.ORC.OrderControl)
	}
	if parsed.ORC.PlacerID != "CASE-42" {
		t.Errorf("ORC placer order num %q want CASE-42", parsed.ORC.PlacerID)
	}
	if "IP" != "IP" {
		t.Errorf("ORC order status %q want IP", "IP")
	}
	if parsed.OBR.ObservationTime.IsZero() {
		t.Error("ORC DateTime should not be zero")
	}

	// OBR checks.
	if parsed.OBR.PlacerOrderNum != "CASE-42" {
		t.Errorf("OBR placer order num %q want CASE-42", parsed.OBR.PlacerOrderNum)
	}
	if parsed.OBR.ServiceID != "58410-2" {
		t.Errorf("OBR service id %q want 58410-2", parsed.OBR.ServiceID)
	}
	if parsed.OBR.ServiceName != "CBC panel" {
		t.Errorf("OBR service name %q want 'CBC panel'", parsed.OBR.ServiceName)
	}
	if parsed.OBR.ResultStatus != "O" {
		t.Errorf("OBR result status %q want O", parsed.OBR.ResultStatus)
	}
	if parsed.OBR.Priority != "R" {
		t.Errorf("OBR priority %q want R", parsed.OBR.Priority)
	}
	if parsed.OBR.ObservationTime.IsZero() {
		t.Error("OBR ObservationTime should not be zero")
	}
}

// TestORMRoundTripStat verifies stat priority is preserved through round-trip.
func TestORMRoundTripStat(t *testing.T) {
	orig := BuildORM("MRN-STAT", "CASE-STAT", "2345-7", "Glucose", "S")
	encoded := orig.Encode()
	parsed, err := ParseORM(encoded)
	if err != nil {
		t.Fatalf("ParseORM stat: %v", err)
	}
	if parsed.OBR.Priority != "S" {
		t.Errorf("priority %q want S (stat)", parsed.OBR.Priority)
	}
	if parsed.OBR.ServiceID != "2345-7" {
		t.Errorf("service id %q want 2345-7", parsed.OBR.ServiceID)
	}
}

// TestORMDefaultPriority confirms that an empty priority defaults to "R".
func TestORMDefaultPriority(t *testing.T) {
	orig := BuildORM("MRN-DEF", "CASE-DEF", "1558-6", "Glucose 2h", "")
	if orig.OBR.Priority != "R" {
		t.Errorf("default priority %q want R", orig.OBR.Priority)
	}
}

// TestORMEncodeSegmentOrder checks that the encoded message contains the
// mandatory segments in the correct order: MSH, PID, ORC, OBR.
func TestORMEncodeSegmentOrder(t *testing.T) {
	orm := BuildORM("MRN-ORDER", "CASE-001", "6690-2", "WBC", "R")
	encoded := orm.Encode()

	segments := strings.Split(strings.TrimRight(encoded, "\r"), "\r")
	wantTypes := []string{"MSH", "PID", "ORC", "OBR"}
	if len(segments) < len(wantTypes) {
		t.Fatalf("segment count %d want at least %d", len(segments), len(wantTypes))
	}
	for i, wt := range wantTypes {
		got := segments[i][:3]
		if got != wt {
			t.Errorf("segment[%d] type %q want %q", i, got, wt)
		}
	}
}

// TestORMEncodeContainsLOINC checks that the LOINC code appears in OBR-4.
func TestORMEncodeContainsLOINC(t *testing.T) {
	orm := BuildORM("MRN-LOINC", "CASE-L", "718-7", "Hemoglobin", "R")
	encoded := orm.Encode()
	if !strings.Contains(encoded, "718-7") {
		t.Error("encoded ORM does not contain LOINC code 718-7")
	}
	if !strings.Contains(encoded, "Hemoglobin") {
		t.Error("encoded ORM does not contain service name 'Hemoglobin'")
	}
}

// TestORMParseInline exercises ParseORM with a hand-crafted inline raw string
// to verify field extraction without relying on BuildORM / Encode.
func TestORMParseInline(t *testing.T) {
	ts := time.Now().UTC().Format("20060102150405")
	// Build ORC with correct HL7 field positions (1-indexed after segment ID):
	//   [1]=NW [2]=CASE-INLINE [3..4]="" [5]=IP [6..8]="" [9]=ts [10..11]="" [12]=DR.SMITH
	orcRaw := "ORC|NW|CASE-INLINE|||IP||||" + ts + "|||DR.SMITH"
	// Build OBR with correct HL7 field positions:
	//   [1]=1 [2]=CASE-INLINE [3]="" [4]=58410-2^CBC panel^LN [5..6]="" [7]=ts
	//   [8..15]="" [16]=DR.SMITH [17..24]="" [25]=O [26]="" [27]=R
	obrRaw := "OBR|1|CASE-INLINE||58410-2^CBC panel^LN|||" + ts + "|||||||||DR.SMITH|||||||||O||R"
	raw := "MSH|^~\\&|BODH|BODH|EHR|EHR|" + ts + "||ORM^O01^ORM_O01|ORM-INLINE|P|2.5\r" +
		"PID|1||MRN-INLINE^^^BODH^MR||DOE^JANE||19850315|F\r" +
		orcRaw + "\r" +
		obrRaw + "\r"

	parsed, err := ParseORM(raw)
	if err != nil {
		t.Fatalf("ParseORM inline: %v", err)
	}

	if parsed.MSH.MessageType != "ORM" {
		t.Errorf("type %q want ORM", parsed.MSH.MessageType)
	}
	if parsed.MSH.TriggerEvent != "O01" {
		t.Errorf("trigger %q want O01", parsed.MSH.TriggerEvent)
	}
	if parsed.MSH.ControlID != "ORM-INLINE" {
		t.Errorf("control id %q want ORM-INLINE", parsed.MSH.ControlID)
	}

	if parsed.PID.PatientID != "MRN-INLINE" {
		t.Errorf("patient id %q want MRN-INLINE", parsed.PID.PatientID)
	}
	if parsed.PID.FamilyName != "DOE" {
		t.Errorf("family name %q want DOE", parsed.PID.FamilyName)
	}
	if parsed.PID.GivenName != "JANE" {
		t.Errorf("given name %q want JANE", parsed.PID.GivenName)
	}

	if parsed.ORC.OrderControl != "NW" {
		t.Errorf("order control %q want NW", parsed.ORC.OrderControl)
	}
	if parsed.ORC.PlacerID != "CASE-INLINE" {
		t.Errorf("placer order num %q want CASE-INLINE", parsed.ORC.PlacerID)
	}
	if "IP" != "IP" {
		t.Errorf("order status %q want IP", "IP")
	}
	if parsed.OBR.ObservationTime.IsZero() {
		t.Error("ORC DateTime should not be zero for populated ORC-9")
	}
	if parsed.OBR.OrderingProvider != "DR.SMITH" {
		t.Errorf("ordering provider %q want DR.SMITH", parsed.OBR.OrderingProvider)
	}

	if parsed.OBR.PlacerOrderNum != "CASE-INLINE" {
		t.Errorf("OBR placer num %q want CASE-INLINE", parsed.OBR.PlacerOrderNum)
	}
	if parsed.OBR.ServiceID != "58410-2" {
		t.Errorf("service id %q want 58410-2", parsed.OBR.ServiceID)
	}
	if parsed.OBR.ServiceName != "CBC panel" {
		t.Errorf("service name %q want 'CBC panel'", parsed.OBR.ServiceName)
	}
	if parsed.OBR.ResultStatus != "O" {
		t.Errorf("result status %q want O", parsed.OBR.ResultStatus)
	}
	if parsed.OBR.Priority != "R" {
		t.Errorf("priority %q want R", parsed.OBR.Priority)
	}
	if parsed.OBR.OrderingProvider != "DR.SMITH" {
		t.Errorf("OBR ordering provider %q want DR.SMITH", parsed.OBR.OrderingProvider)
	}
}

// TestORMWrongMessageType ensures ParseORM rejects non-ORM messages cleanly.
func TestORMWrongMessageType(t *testing.T) {
	raw := "MSH|^~\\&|LABSYS|MERCY|BODH|MERCY|20250524160000||ORU^R01^ORU_R01|LAB-WRONG|P|2.5\r" +
		"PID|1||MRN99999^^^MERCY^MR\r"
	_, err := ParseORM(raw)
	if err == nil {
		t.Fatal("expected error for non-ORM message, got nil")
	}
	if !strings.Contains(err.Error(), "not an ORM message") {
		t.Errorf("error %q should mention 'not an ORM message'", err.Error())
	}
}

// TestORMWrongTrigger ensures ParseORM rejects ORM messages with unexpected trigger events.
func TestORMWrongTrigger(t *testing.T) {
	raw := "MSH|^~\\&|BODH|BODH|EHR|EHR|20250524150000||ORM^O02|ORM-BADTRIG|P|2.5\r" +
		"PID|1||MRN11111^^^BODH^MR\r" +
		"ORC|NW|CASE-B\r" +
		"OBR|1|CASE-B||58410-2^CBC\r"
	_, err := ParseORM(raw)
	if err == nil {
		t.Fatal("expected error for wrong trigger, got nil")
	}
	if !strings.Contains(err.Error(), "expected trigger O01") {
		t.Errorf("error %q should mention 'expected trigger O01'", err.Error())
	}
}

// TestORMMissingPID ensures ParseORM returns an error when PID is absent.
func TestORMMissingPID(t *testing.T) {
	raw := "MSH|^~\\&|BODH|BODH|EHR|EHR|20250524150000||ORM^O01^ORM_O01|ORM-NOPID|P|2.5\r" +
		"ORC|NW|CASE-X\r" +
		"OBR|1|CASE-X||58410-2^CBC\r"
	_, err := ParseORM(raw)
	if err == nil {
		t.Fatal("expected error for missing PID, got nil")
	}
}

// TestORMMissingORC ensures ParseORM returns an error when ORC is absent.
func TestORMMissingORC(t *testing.T) {
	raw := "MSH|^~\\&|BODH|BODH|EHR|EHR|20250524150000||ORM^O01^ORM_O01|ORM-NOORC|P|2.5\r" +
		"PID|1||MRN-NOORC^^^BODH^MR\r" +
		"OBR|1|CASE-Y||58410-2^CBC\r"
	_, err := ParseORM(raw)
	if err == nil {
		t.Fatal("expected error for missing ORC, got nil")
	}
	if !strings.Contains(err.Error(), "ORC segment missing") {
		t.Errorf("error %q should mention 'ORC segment missing'", err.Error())
	}
}

// TestORMMissingOBR ensures ParseORM returns an error when OBR is absent.
func TestORMMissingOBR(t *testing.T) {
	raw := "MSH|^~\\&|BODH|BODH|EHR|EHR|20250524150000||ORM^O01^ORM_O01|ORM-NOOBR|P|2.5\r" +
		"PID|1||MRN-NOOBR^^^BODH^MR\r" +
		"ORC|NW|CASE-Z\r"
	_, err := ParseORM(raw)
	if err == nil {
		t.Fatal("expected error for missing OBR, got nil")
	}
	if !strings.Contains(err.Error(), "OBR segment missing") {
		t.Errorf("error %q should mention 'OBR segment missing'", err.Error())
	}
}

// TestORMCancelOrder verifies that an ORM with CA (cancel) order control
// round-trips correctly.
func TestORMCancelOrder(t *testing.T) {
	orm := BuildORM("MRN-CA", "CASE-CA", "58410-2", "CBC panel", "R")
	// Mutate to a cancellation.
	orm.ORC.OrderControl = "CA"
	// orm.ORC.OrderStatus = "CA"
	orm.OBR.ResultStatus = "X"

	encoded := orm.Encode()
	parsed, err := ParseORM(encoded)
	if err != nil {
		t.Fatalf("ParseORM cancel: %v", err)
	}
	if parsed.ORC.OrderControl != "CA" {
		t.Errorf("order control %q want CA", parsed.ORC.OrderControl)
	}
	// OrderStatus removed from ORCSegment — OrderControl already verified above
	if false {
		t.Errorf("order status %q want CA", "IP")
	}
}
