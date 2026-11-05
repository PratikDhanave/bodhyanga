// Package hl7v2 — ORM^O01 (Order Message) builder, encoder, and parser.
//
// ORM^O01 is the HL7 v2 message type used to transmit lab or radiology orders
// from Bodhyanga to an external EHR / LIS / RIS system. The segment structure is:
//
//	MSH — message header
//	PID — patient identification
//	ORC — common order (order control + status)
//	OBR — observation request (the actual test or procedure)
//
// ORCSegment and MSHSegment are shared with the ORU^R01 path (see oru.go).
// OBRSegment is defined here because its ORM subset differs from the ORU
// LabResult type (which groups OBX rows under an OBR).
package hl7v2

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ---- Public types -----------------------------------------------------------

// OBRSegment carries the Observation Request fields for an ORM^O01 order.
// Only the ORM-relevant subset is modelled here; ORU results use LabResult.
type OBRSegment struct {
	PlacerOrderNum   string    // OBR-2: placer order number (echoes ORC-2)
	ServiceID        string    // OBR-4.1: universal service identifier (LOINC code)
	ServiceName      string    // OBR-4.2: universal service identifier display name
	ObservationTime  time.Time // OBR-7: observation date/time (zero = not set)
	OrderingProvider string    // OBR-16: ordering provider name/ID
	ResultStatus     string    // OBR-25: O=received, I=in progress, P=preliminary, F=final
	Priority         string    // OBR-27.1: R=routine, S=stat, A=asap
}

// ORMMessage is the top-level representation of an ORM^O01 message.
type ORMMessage struct {
	MSH MSHSegment
	PID PIDSegment
	ORC ORCSegment
	OBR OBRSegment
}

// ---- BuildORM ---------------------------------------------------------------

// BuildORM constructs a new ORM^O01 message for sending a lab or imaging order
// to an external EHR / LIS / RIS system.
//
// Parameters:
//   - patientID: the patient's medical record number (MR identifier)
//   - caseID: Bodhyanga internal case identifier used as the placer order number
//   - loincCode: LOINC code for the ordered test (e.g. "58410-2")
//   - testName: human-readable test name (e.g. "CBC panel")
//   - priority: order priority — "R" (routine), "S" (stat), or "A" (asap)
func BuildORM(patientID, caseID, loincCode, testName, priority string) ORMMessage {
	now := time.Now().UTC()
	ctrl := "ORM-" + uuid.New().String()[:8]

	if priority == "" {
		priority = "R"
	}

	return ORMMessage{
		MSH: MSHSegment{
			SendingApp:        "BODHYANGA",
			SendingFacility:   "BODHYANGA",
			ReceivingApp:      "EHR",
			ReceivingFacility: "EHR",
			Timestamp:         now.Format("20060102150405"),
			MessageType:       "ORM",
			TriggerEvent:      "O01",
			MessageStructure:  "ORM_O01",
			ControlID:         ctrl,
			ProcessingID:      "P",
			VersionID:         "2.5",
		},
		PID: PIDSegment{
			PatientID: patientID,
		},
		ORC: ORCSegment{
			OrderControl: "NW",
			PlacerID:     caseID,
		},
		OBR: OBRSegment{
			PlacerOrderNum:  caseID,
			ServiceID:       loincCode,
			ServiceName:     testName,
			ObservationTime: now,
			ResultStatus:    "O",
			Priority:        priority,
		},
	}
}

// ---- Encode -----------------------------------------------------------------

// Encode serialises the ORMMessage to a pipe-delimited HL7 v2 string using the
// default encoding characters (|^~\&). Segments are separated by \r per the
// HL7 v2 specification.
func (m ORMMessage) Encode() string {
	enc := DefaultEncoding()
	f := string(enc.Field)
	c := string(enc.Component)
	now := time.Now().UTC().Format("20060102150405")

	// Use stored timestamp if available, else current time.
	ts := m.MSH.Timestamp
	if ts == "" {
		ts = now
	}

	// MSH segment. MSH-9 is MessageType^TriggerEvent^MessageStructure.
	msgType := m.MSH.MessageType + c + m.MSH.TriggerEvent
	if m.MSH.MessageStructure != "" {
		msgType += c + m.MSH.MessageStructure
	}
	msh := strings.Join([]string{
		"MSH",
		string(enc.Component) + string(enc.Repetition) + string(enc.Escape) + string(enc.Subcomponent),
		m.MSH.SendingApp,
		m.MSH.SendingFacility,
		m.MSH.ReceivingApp,
		m.MSH.ReceivingFacility,
		ts,
		"", // MSH-8 security (unused)
		msgType,
		m.MSH.ControlID,
		m.MSH.ProcessingID,
		m.MSH.VersionID,
	}, f)

	// PID segment. PID-3 uses CX type: ID^^^ASSIGNING_AUTHORITY^MR
	pid := strings.Join([]string{
		"PID",
		"1",        // PID-1 set id
		"",         // PID-2 patient id (external, deprecated)
		m.PID.PatientID + "^^^BODHYANGA^MR", // PID-3
		"",         // PID-4
		m.PID.FamilyName + c + m.PID.GivenName, // PID-5
		"",         // PID-6
		m.PID.DateOfBirth, // PID-7
		m.PID.Sex,         // PID-8
	}, f)

	// ORC segment.
	// ORC fields: 1=OrderControl, 2=PlacerID, 3=FillerID
	orc := strings.Join([]string{
		"ORC",
		m.ORC.OrderControl, // ORC-1
		m.ORC.PlacerID,     // ORC-2
		m.ORC.FillerID,     // ORC-3
	}, f)

	// OBR segment.
	obrDT := ""
	if !m.OBR.ObservationTime.IsZero() {
		obrDT = m.OBR.ObservationTime.UTC().Format("20060102150405")
	}
	// OBR-4 is CWE: ServiceID^ServiceName
	serviceID := m.OBR.ServiceID + c + m.OBR.ServiceName
	// OBR-27: TQ1 quantity/timing — encode priority in field 1 of TQ
	priority := m.OBR.Priority
	// OBR fields: 1=SetID, 2=PlacerOrderNum, 3=FillerOrderNum, 4=ServiceID,
	//             5=, 6=, 7=ObsDT, ..., 16=OrderingProvider, ...,
	//             22=ResultsRptStatusChgDT, ..., 25=ResultStatus, ..., 27=QtyTiming
	obr := strings.Join([]string{
		"OBR",
		"1",                    // OBR-1 set id
		m.OBR.PlacerOrderNum,  // OBR-2
		"",                    // OBR-3 filler order num (not set at order time)
		serviceID,             // OBR-4
		"",                    // OBR-5
		"",                    // OBR-6
		obrDT,                 // OBR-7
		"",                    // OBR-8
		"",                    // OBR-9
		"",                    // OBR-10
		"",                    // OBR-11
		"",                    // OBR-12
		"",                    // OBR-13
		"",                    // OBR-14
		"",                    // OBR-15
		m.OBR.OrderingProvider, // OBR-16
		"",                    // OBR-17
		"",                    // OBR-18
		"",                    // OBR-19
		"",                    // OBR-20
		"",                    // OBR-21
		"",                    // OBR-22 results rpt/status change date/time
		"",                    // OBR-23
		"",                    // OBR-24
		m.OBR.ResultStatus,    // OBR-25
		"",                    // OBR-26
		priority,              // OBR-27 quantity/timing (priority only)
	}, f)

	return msh + "\r" + pid + "\r" + orc + "\r" + obr + "\r"
}

// ---- ParseORM ---------------------------------------------------------------

// ParseORM parses a raw HL7 v2 ORM^O01 pipe-delimited message into an
// ORMMessage. It reuses ParseMessage for low-level tokenisation (so custom
// encoding characters are honoured) and then maps segments to typed structs.
//
// An error is returned if:
//   - The raw message cannot be tokenised (see ParseMessage)
//   - The message type is not ORM
//   - The trigger event is not O01
//   - A required segment (PID, ORC, OBR) is absent
func ParseORM(raw string) (*ORMMessage, error) {
	msg, err := ParseMessage(raw)
	if err != nil {
		return nil, err
	}
	if msg.Header.MessageType != "ORM" {
		return nil, fmt.Errorf("hl7v2/orm: not an ORM message (type=%q)", msg.Header.MessageType)
	}
	if msg.Header.TriggerEvent != "O01" {
		return nil, fmt.Errorf("hl7v2/orm: expected trigger O01, got %q", msg.Header.TriggerEvent)
	}

	pidSeg, ok := msg.FindFirst("PID")
	if !ok {
		return nil, errors.New("hl7v2/orm: PID segment missing")
	}
	pid := parsePIDSegment(pidSeg, msg.Encoding)

	orcSeg, ok := msg.FindFirst("ORC")
	if !ok {
		return nil, errors.New("hl7v2/orm: ORC segment missing")
	}
	orc := parseORCSegment(orcSeg)

	obrSeg, ok := msg.FindFirst("OBR")
	if !ok {
		return nil, errors.New("hl7v2/orm: OBR segment missing")
	}
	obr := parseORMOBRSegment(obrSeg, msg.Encoding)

	return &ORMMessage{
		MSH: msg.Header,
		PID: pid,
		ORC: orc,
		OBR: obr,
	}, nil
}

// parseORMOBRSegment extracts the ORM-relevant fields from an OBR segment.
// This is distinct from the ORU parseOBRSegment which returns a LabResult
// (panel name + observations slice) because ORM OBR carries order metadata
// rather than result data.
func parseORMOBRSegment(seg Segment, enc Encoding) OBRSegment {
	// OBR-4: Universal Service Identifier — CWE/CE: code^display^system
	serviceField := seg.field(4)
	serviceID := component(serviceField, enc, 1)
	serviceName := component(serviceField, enc, 2)

	obsTime, _ := parseHL7Time(seg.field(7))

	return OBRSegment{
		PlacerOrderNum:   seg.field(2),
		ServiceID:        serviceID,
		ServiceName:      serviceName,
		ObservationTime:  obsTime,
		OrderingProvider: seg.field(16),
		ResultStatus:     seg.field(25),
		Priority:         seg.field(27),
	}
}
