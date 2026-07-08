// Package hl7v2 — ORU^R01 (Unsolicited Observation Result) parser.
//
// ORU^R01 is the HL7 v2 message type used to transmit lab results from an LIS
// (Laboratory Information System) to a clinical application. The structure is:
//
//	MSH — message header
//	PID — patient identification
//	[ORC] — common order (optional)
//	OBR — observation request (one per lab panel)
//	OBX — observation result (one per individual test value)
//
// ParseORU delegates MSH tokenisation to the existing ParseMessage path so
// encoding-character negotiation (MSH-1 / MSH-2) is handled in one place.
package hl7v2

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---- Public types -----------------------------------------------------------

// MSHSegment is a thin alias over MSH so ORUMessage uses the same type name
// used in HL7 documentation. It is identical to the existing MSH struct.
type MSHSegment = MSH

// PIDSegment carries the patient identification fields extracted from PID.
type PIDSegment struct {
	PatientID   string // PID-3: preferred MR-typed identifier (see pickPatientID)
	FamilyName  string // PID-5.1
	GivenName   string // PID-5.2
	DateOfBirth string // PID-7 (HL7 timestamp string; not parsed to time.Time)
	Sex         string // PID-8
}

// ORCSegment carries the optional Common Order fields used to correlate a lab
// result with its original order.
type ORCSegment struct {
	OrderControl string // ORC-1 (NW=new, CA=cancel, RE=result, ...)
	PlacerID     string // ORC-2: placer (ordering system) order number
	FillerID     string // ORC-3: filler (LIS) order number
}

// Observation is a single measured test value from an OBX segment.
type Observation struct {
	LOINCCode      string    // OBX-3.1 (LOINC code)
	TestName       string    // OBX-3.2 (display text)
	Value          string    // OBX-5
	Units          string    // OBX-6.1
	ReferenceRange string    // OBX-7
	AbnormalFlag   string    // OBX-8: H, L, HH, LL, N, etc.
	Status         string    // OBX-11: F=final, P=preliminary, C=corrected
	ObservedAt     time.Time // OBX-14 (zero if absent)
	IsCritical     bool      // true when AbnormalFlag is HH or LL
}

// LabResult groups an OBR (observation request) with all of its OBX rows.
// A single ORU^R01 message may carry multiple LabResults (one per panel).
type LabResult struct {
	PanelName    string    // OBR-4.2 (universal service ID display name)
	OrderNumber  string    // OBR-3 (filler order number)
	CollectedAt  time.Time // OBR-7 (observation date/time; zero if absent)
	Observations []Observation
}

// ORUMessage is the top-level parsed representation of an ORU^R01 message.
type ORUMessage struct {
	MSH     MSHSegment
	PID     PIDSegment
	ORC     *ORCSegment // nil when no ORC segment is present
	Results []LabResult
}

// ---- ParseORU ---------------------------------------------------------------

// ParseORU parses a raw HL7 v2 ORU^R01 message and returns a structured
// ORUMessage. It reuses ParseMessage for low-level tokenisation (so custom
// encoding characters are honoured) and then maps segments to typed structs.
//
// An error is returned if:
//   - The raw message cannot be tokenised (see ParseMessage)
//   - The message type is not ORU
//   - The trigger event is not R01
//   - A required segment (PID, at least one OBR) is absent
func ParseORU(raw string) (*ORUMessage, error) {
	msg, err := ParseMessage(raw)
	if err != nil {
		return nil, err
	}
	if msg.Header.MessageType != "ORU" {
		return nil, fmt.Errorf("hl7v2/oru: not an ORU message (type=%q)", msg.Header.MessageType)
	}
	if msg.Header.TriggerEvent != "R01" {
		return nil, fmt.Errorf("hl7v2/oru: expected trigger R01, got %q", msg.Header.TriggerEvent)
	}

	pidSeg, ok := msg.FindFirst("PID")
	if !ok {
		return nil, errors.New("hl7v2/oru: PID segment missing")
	}
	pid := parsePIDSegment(pidSeg, msg.Encoding)

	out := &ORUMessage{
		MSH: msg.Header,
		PID: pid,
	}

	// ORC is optional.
	if orcSeg, ok := msg.FindFirst("ORC"); ok {
		orc := parseORCSegment(orcSeg)
		out.ORC = &orc
	}

	// Walk segments in order; group OBX rows under their nearest preceding OBR.
	var current *LabResult
	for _, seg := range msg.Segments {
		switch seg.Type {
		case "OBR":
			lr := parseOBRSegment(seg, msg.Encoding)
			out.Results = append(out.Results, lr)
			current = &out.Results[len(out.Results)-1]
		case "OBX":
			if current == nil {
				// OBX without a preceding OBR — create an anonymous panel.
				out.Results = append(out.Results, LabResult{PanelName: "UNKNOWN"})
				current = &out.Results[len(out.Results)-1]
			}
			obs := parseOBXSegment(seg, msg.Encoding)
			current.Observations = append(current.Observations, obs)
		}
	}

	if len(out.Results) == 0 {
		return nil, errors.New("hl7v2/oru: no OBR/OBX segments found")
	}
	return out, nil
}

// ---- Segment parsers --------------------------------------------------------

func parsePIDSegment(seg Segment, enc Encoding) PIDSegment {
	name := seg.field(5) // PID-5: XPN type — family^given
	return PIDSegment{
		PatientID:   pickPatientID(seg, enc),
		FamilyName:  component(name, enc, 1),
		GivenName:   component(name, enc, 2),
		DateOfBirth: seg.field(7),
		Sex:         seg.field(8),
	}
}

func parseORCSegment(seg Segment) ORCSegment {
	return ORCSegment{
		OrderControl: seg.field(1),
		PlacerID:     seg.field(2),
		FillerID:     seg.field(3),
	}
}

func parseOBRSegment(seg Segment, enc Encoding) LabResult {
	// OBR-4: Universal Service Identifier — CWE/CE type: code^display^system
	serviceID := seg.field(4)
	panelName := component(serviceID, enc, 2)
	if panelName == "" {
		panelName = component(serviceID, enc, 1) // fall back to code
	}

	collectedAt, _ := parseHL7Time(seg.field(7)) // OBR-7: observation date/time

	return LabResult{
		PanelName:   panelName,
		OrderNumber: seg.field(3), // OBR-3: filler order number
		CollectedAt: collectedAt,
	}
}

func parseOBXSegment(seg Segment, enc Encoding) Observation {
	// OBX-3: Observation Identifier — CWE/CE: code^display^coding_system
	loincField := seg.field(3)
	loincCode := component(loincField, enc, 1)
	testName := component(loincField, enc, 2)

	// OBX-6: Units — CWE/CE: unit_code^display; we want display preferably.
	unitsField := seg.field(6)
	units := component(unitsField, enc, 1)
	if u2 := component(unitsField, enc, 2); u2 != "" {
		units = u2
	}

	abnormalFlag := strings.ToUpper(strings.TrimSpace(seg.field(8)))
	isCritical := abnormalFlag == "HH" || abnormalFlag == "LL"

	observedAt, _ := parseHL7Time(seg.field(14))

	return Observation{
		LOINCCode:      loincCode,
		TestName:       testName,
		Value:          seg.field(5),
		Units:          units,
		ReferenceRange: seg.field(7),
		AbnormalFlag:   abnormalFlag,
		Status:         seg.field(11),
		ObservedAt:     observedAt,
		IsCritical:     isCritical,
	}
}
