// Package hl7v2 is a minimal HL7 v2.x pipe-delimited message parser focused
// on the ADT (Admit/Discharge/Transfer) subset most US hospital integrations
// still use as a "front door". It is intentionally not a full HL7 v2
// implementation: only MSH, EVN, PID, PV1 and DG1 are parsed, and only the
// fields used to construct a medical.ADTEvent are surfaced as accessors.
//
// Delimiters are read from the inbound MSH-1 / MSH-2 (per the HL7 spec)
// rather than hardcoded — many trading partners use the defaults (|^~\&) but
// the parser will honour custom encoding characters too.
package hl7v2

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// Encoding holds the five delimiter characters defined by MSH-1 and MSH-2.
//
//	Field          MSH-1   default |
//	Component      MSH-2   default ^
//	Repetition     MSH-2   default ~
//	Escape         MSH-2   default \
//	Subcomponent   MSH-2   default &
type Encoding struct {
	Field        byte
	Component    byte
	Repetition   byte
	Escape       byte
	Subcomponent byte
}

// DefaultEncoding returns the standard HL7 v2 delimiter set.
func DefaultEncoding() Encoding {
	return Encoding{Field: '|', Component: '^', Repetition: '~', Escape: '\\', Subcomponent: '&'}
}

// Segment is a parsed HL7 segment: a three-letter type tag plus its fields.
// Fields[0] always holds the segment ID itself, so PID-3 is Fields[3]
// (matching the segment-number convention used in HL7 documentation).
//
// MSH is the one exception — see ParseMessage / Message.MSH for the offset
// handling there.
type Segment struct {
	Type   string
	Fields []string
}

// Message is a parsed HL7 v2 message: header plus all subsequent segments.
type Message struct {
	Encoding Encoding
	Segments []Segment
	// MSH is the message header, conveniently surfaced. SendingApp / ReceivingApp
	// / MessageType / ControlID / Timestamp are pre-extracted for convenience.
	Header MSH
}

// MSH is the parsed Message Header. Field positions reference the HL7 spec
// (e.g. MSH-9 is MessageType, MSH-10 is MessageControlID).
type MSH struct {
	SendingApp        string // MSH-3
	SendingFacility   string // MSH-4
	ReceivingApp      string // MSH-5
	ReceivingFacility string // MSH-6
	Timestamp         string // MSH-7 — YYYYMMDDHHMMSS[+ZZZZ]
	MessageType       string // MSH-9.1 (e.g. ADT)
	TriggerEvent      string // MSH-9.2 (e.g. A01)
	MessageStructure  string // MSH-9.3 (e.g. ADT_A01) — optional
	ControlID         string // MSH-10
	ProcessingID      string // MSH-11
	VersionID         string // MSH-12 (e.g. 2.5)
}

// ParseMessage parses a raw HL7 v2 pipe-delimited message. Segments may be
// separated by \r (canonical), \n, or \r\n — real-world traffic varies.
//
// The MSH-1 field separator and MSH-2 encoding characters are honoured: the
// parser reads them off the wire before tokenising any other segment, so a
// message that uses # instead of | will still parse.
func ParseMessage(raw string) (*Message, error) {
	if raw == "" {
		return nil, errors.New("hl7v2: empty message")
	}
	// Normalise the newline soup but keep \r as the canonical segment
	// terminator. Some senders use \n, some \r\n; we collapse both to \r.
	raw = strings.ReplaceAll(raw, "\r\n", "\r")
	raw = strings.ReplaceAll(raw, "\n", "\r")
	raw = strings.TrimRight(raw, "\r")

	if !strings.HasPrefix(raw, "MSH") {
		return nil, errors.New("hl7v2: message does not start with MSH segment")
	}
	if len(raw) < 8 {
		return nil, errors.New("hl7v2: MSH too short to contain encoding characters")
	}

	enc := Encoding{
		Field:        raw[3],
		Component:    raw[4],
		Repetition:   raw[5],
		Escape:       raw[6],
		Subcomponent: raw[7],
	}

	segStrs := strings.Split(raw, "\r")
	out := &Message{Encoding: enc, Segments: make([]Segment, 0, len(segStrs))}
	for i, s := range segStrs {
		if s == "" {
			continue
		}
		seg, err := parseSegment(s, enc, i == 0)
		if err != nil {
			return nil, err
		}
		out.Segments = append(out.Segments, seg)
	}
	if len(out.Segments) == 0 || out.Segments[0].Type != "MSH" {
		return nil, errors.New("hl7v2: missing MSH segment after parse")
	}
	hdr, err := parseMSH(out.Segments[0], enc)
	if err != nil {
		return nil, err
	}
	out.Header = hdr
	return out, nil
}

// parseSegment splits a single segment string on the field delimiter.
//
// MSH is special-cased: MSH-1 is the field separator itself (and is "|"),
// so the conventional MSH-2 (encoding characters) appears at Fields[2]. We
// follow the HL7 convention by inserting the field separator string as
// Fields[1] so segment-number indexing stays consistent across all segment
// types.
func parseSegment(raw string, enc Encoding, isMSH bool) (Segment, error) {
	sep := string(enc.Field)
	if isMSH {
		// Strip "MSH" + field separator and split the rest. Insert the
		// field separator at Fields[1] so MSH-3 lands at Fields[3].
		if len(raw) < 4 {
			return Segment{}, errors.New("hl7v2: malformed MSH segment")
		}
		rest := raw[4:]
		parts := strings.Split(rest, sep)
		fields := make([]string, 0, len(parts)+2)
		fields = append(fields, "MSH", sep)
		fields = append(fields, parts...)
		return Segment{Type: "MSH", Fields: fields}, nil
	}
	parts := strings.Split(raw, sep)
	if len(parts[0]) != 3 {
		return Segment{}, fmt.Errorf("hl7v2: segment type %q is not 3 chars", parts[0])
	}
	return Segment{Type: parts[0], Fields: parts}, nil
}

// parseMSH pulls the convenience header fields out of the MSH segment.
func parseMSH(seg Segment, enc Encoding) (MSH, error) {
	if seg.Type != "MSH" {
		return MSH{}, errors.New("hl7v2: expected MSH segment")
	}
	h := MSH{
		SendingApp:        seg.field(3),
		SendingFacility:   seg.field(4),
		ReceivingApp:      seg.field(5),
		ReceivingFacility: seg.field(6),
		Timestamp:         seg.field(7),
		ControlID:         seg.field(10),
		ProcessingID:      seg.field(11),
		VersionID:         seg.field(12),
	}
	// MSH-9 is the Message Type, components 1=MessageCode, 2=TriggerEvent,
	// 3=MessageStructure (optional, e.g. "ADT_A01"). Split on the component
	// delimiter from the encoding chars.
	if mt := seg.field(9); mt != "" {
		comps := strings.Split(mt, string(enc.Component))
		if len(comps) > 0 {
			h.MessageType = comps[0]
		}
		if len(comps) > 1 {
			h.TriggerEvent = comps[1]
		}
		if len(comps) > 2 {
			h.MessageStructure = comps[2]
		}
	}
	if h.MessageType == "" {
		return MSH{}, errors.New("hl7v2: MSH-9 message type missing")
	}
	if h.ControlID == "" {
		return MSH{}, errors.New("hl7v2: MSH-10 control id missing")
	}
	return h, nil
}

// field returns the value at the 1-based HL7 field index, or "" if the
// segment doesn't have that many fields.
func (s Segment) field(n int) string {
	if n < 0 || n >= len(s.Fields) {
		return ""
	}
	return s.Fields[n]
}

// FindSegments returns every segment of the given type in document order.
// Useful for repeating segments like DG1.
func (m *Message) FindSegments(typ string) []Segment {
	out := make([]Segment, 0)
	for _, s := range m.Segments {
		if s.Type == typ {
			out = append(out, s)
		}
	}
	return out
}

// FindFirst returns the first segment of the given type, or false.
func (m *Message) FindFirst(typ string) (Segment, bool) {
	for _, s := range m.Segments {
		if s.Type == typ {
			return s, true
		}
	}
	return Segment{}, false
}

// component returns the n-th 1-based component of a field, split on the
// component delimiter.
func component(field string, enc Encoding, n int) string {
	if field == "" || n < 1 {
		return ""
	}
	parts := strings.Split(field, string(enc.Component))
	if n-1 >= len(parts) {
		return ""
	}
	return parts[n-1]
}

// repetition returns the n-th 1-based repetition of a field, split on the
// repetition delimiter. n=1 returns everything before the first ~ etc.
func repetition(field string, enc Encoding, n int) string {
	if field == "" || n < 1 {
		return ""
	}
	parts := strings.Split(field, string(enc.Repetition))
	if n-1 >= len(parts) {
		return ""
	}
	return parts[n-1]
}

// repetitions returns every repetition of the field.
func repetitions(field string, enc Encoding) []string {
	if field == "" {
		return nil
	}
	return strings.Split(field, string(enc.Repetition))
}

// parseHL7Time parses an HL7 v2 timestamp. The spec allows YYYY,
// YYYYMM, YYYYMMDD, YYYYMMDDHH, YYYYMMDDHHMM, YYYYMMDDHHMMSS, any of which
// may have a +ZZZZ / -ZZZZ timezone suffix. We try the most-specific layouts
// first and fall back. Empty string returns zero time, no error.
func parseHL7Time(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	// Split out optional timezone.
	tz := ""
	for i := len(s) - 5; i >= 0; i-- {
		if s[i] == '+' || s[i] == '-' {
			// A timezone marker must be followed by exactly 4 digits and
			// sit near the end — guards against minus signs inside the
			// numeric body (which shouldn't occur but defensive).
			if len(s)-i == 5 {
				tz = s[i:]
				s = s[:i]
				break
			}
		}
	}
	layouts := []string{
		"20060102150405",
		"200601021504",
		"2006010215",
		"20060102",
		"200601",
		"2006",
	}
	for _, l := range layouts {
		if len(s) == len(l) {
			if tz != "" {
				if t, err := time.Parse(l+"-0700", s+tz); err == nil {
					return t.UTC(), nil
				}
			}
			if t, err := time.Parse(l, s); err == nil {
				return t.UTC(), nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("hl7v2: unparseable timestamp %q", s)
}

// pickPatientID walks PID-3 (repeatable CX-typed identifier list) and
// returns the best identifier. We prefer the repetition whose CX-5
// (Identifier Type Code) is "MR" (Medical Record Number), and fall back to
// the first repetition's CX-1. This matches what most hospital trading
// partners send: the MR number is the durable one across systems.
func pickPatientID(pid Segment, enc Encoding) string {
	field := pid.field(3)
	if field == "" {
		return ""
	}
	reps := repetitions(field, enc)
	// First pass: look for type code MR.
	for _, r := range reps {
		if strings.EqualFold(component(r, enc, 5), "MR") {
			if id := component(r, enc, 1); id != "" {
				return id
			}
		}
	}
	// Fallback: first repetition's CX-1.
	return component(reps[0], enc, 1)
}

// MessageToADTEvent maps a parsed Message onto Bodhyanga's medical.ADTEvent.
//
// Mapping summary:
//
//	medical.ADTEvent.EventID    <- MSH-10 (Message Control ID)
//	medical.ADTEvent.PatientID  <- PID-3 (MR-typed CX-1 preferred)
//	medical.ADTEvent.Type       <- MSH-9.2 trigger event (A01/A04→admit, A03→discharge, A02→transfer, A08→admit (update))
//	medical.ADTEvent.Facility   <- PV1-3.1 (point of care from Assigned Patient Location)
//	medical.ADTEvent.Unit       <- PV1-3.3 (bed)
//	medical.ADTEvent.OccurredAt <- EVN-2 if present, else MSH-7
//	medical.ADTEvent.PrimaryDx  <- first DG1 with DG1-6 priority "W" (working) else first DG1's DG1-3.1
func MessageToADTEvent(msg *Message) (medical.ADTEvent, error) {
	if msg == nil {
		return medical.ADTEvent{}, errors.New("hl7v2: nil message")
	}
	if msg.Header.MessageType != "ADT" {
		return medical.ADTEvent{}, fmt.Errorf("hl7v2: not an ADT message (type=%q)", msg.Header.MessageType)
	}
	evType, err := triggerToType(msg.Header.TriggerEvent)
	if err != nil {
		return medical.ADTEvent{}, err
	}

	pid, ok := msg.FindFirst("PID")
	if !ok {
		return medical.ADTEvent{}, errors.New("hl7v2: PID segment missing")
	}
	patientID := pickPatientID(pid, msg.Encoding)
	if patientID == "" {
		return medical.ADTEvent{}, errors.New("hl7v2: PID-3 contains no usable patient identifier")
	}

	occurredAt := msg.Header.Timestamp
	if evn, ok := msg.FindFirst("EVN"); ok {
		if v := evn.field(2); v != "" {
			occurredAt = v
		}
	}
	when, err := parseHL7Time(occurredAt)
	if err != nil {
		return medical.ADTEvent{}, err
	}

	facility, unit := "", ""
	if pv1, ok := msg.FindFirst("PV1"); ok {
		loc := pv1.field(3)
		facility = component(loc, msg.Encoding, 1)
		unit = component(loc, msg.Encoding, 3)
	}

	dx := primaryDiagnosis(msg)

	return medical.ADTEvent{
		EventID:    msg.Header.ControlID,
		PatientID:  patientID,
		Type:       evType,
		Facility:   facility,
		Unit:       unit,
		OccurredAt: when,
		PrimaryDx:  dx,
	}, nil
}

// primaryDiagnosis picks the diagnosis code: first DG1 with DG1-6=="W"
// (working) wins, otherwise the first DG1 segment's DG1-3.1 (Diagnosis Code).
func primaryDiagnosis(msg *Message) string {
	dgs := msg.FindSegments("DG1")
	if len(dgs) == 0 {
		return ""
	}
	enc := msg.Encoding
	for _, d := range dgs {
		if strings.EqualFold(d.field(6), "W") {
			if c := component(d.field(3), enc, 1); c != "" {
				return c
			}
		}
	}
	// Fallback: first DG1 code component.
	return component(dgs[0].field(3), enc, 1)
}

// triggerToType maps an HL7 v2 ADT trigger event code to medical.ADTEventType.
// We support the subset the README calls out as the "front door": A01 admit,
// A03 discharge, A04 register-as-outpatient, A08 update (treated as admit
// because it carries a refreshed encounter snapshot). A02 maps to transfer.
func triggerToType(trig string) (medical.ADTEventType, error) {
	switch strings.ToUpper(trig) {
	case "A01", "A04", "A08":
		return medical.ADTAdmit, nil
	case "A03":
		return medical.ADTDischarge, nil
	case "A02":
		return medical.ADTTransfer, nil
	default:
		return "", fmt.Errorf("hl7v2: unsupported ADT trigger event %q", trig)
	}
}

// BuildACK constructs an HL7 v2 ACK message for the supplied original MSH.
// code is one of: AA (Accept), AE (Error), AR (Reject). The ACK uses the
// default delimiter set rather than echoing the inbound encoding chars —
// safe because the field/component delimiters in an ACK string will not
// collide with the encoded body text in our usage.
//
// Per HL7 spec the ACK MSA references the original message's control ID so
// the sender can correlate. The sender and receiver are flipped relative to
// the inbound MSH (we're answering, so their receiver is now our sender).
func BuildACK(originalMSH MSH, code, text string) string {
	enc := DefaultEncoding()
	now := time.Now().UTC().Format("20060102150405")
	// MSH for the ACK. Note MSH-9 is ACK^A01^ACK (echo the inbound trigger
	// where applicable; bare ACK is also valid).
	trigger := originalMSH.TriggerEvent
	if trigger == "" {
		trigger = "A01"
	}
	version := originalMSH.VersionID
	if version == "" {
		version = "2.5"
	}
	processing := originalMSH.ProcessingID
	if processing == "" {
		processing = "P"
	}
	ctrl := "ACK-" + originalMSH.ControlID
	if originalMSH.ControlID == "" {
		ctrl = "ACK-" + now
	}
	msh := strings.Join([]string{
		"MSH",
		string(enc.Component) + string(enc.Repetition) + string(enc.Escape) + string(enc.Subcomponent),
		originalMSH.ReceivingApp,
		originalMSH.ReceivingFacility,
		originalMSH.SendingApp,
		originalMSH.SendingFacility,
		now,
		"",
		"ACK" + string(enc.Component) + trigger + string(enc.Component) + "ACK",
		ctrl,
		processing,
		version,
	}, string(enc.Field))
	msa := strings.Join([]string{
		"MSA",
		code,
		originalMSH.ControlID,
		text,
	}, string(enc.Field))
	return msh + "\r" + msa + "\r"
}
