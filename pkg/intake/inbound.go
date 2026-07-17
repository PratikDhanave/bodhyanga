package intake

// RejectReason is a closed taxonomy of why an inbound clinical message could
// not be translated into a PatientCase. The reason is the ONLY thing that may
// reach an audit event or log — never the message content (spec FR-005/FR-006).
type RejectReason string

const (
	ReasonMalformed          RejectReason = "malformed"           // not parseable as the declared format
	ReasonUnsupportedType    RejectReason = "unsupported_type"    // parseable, but wrong message class (e.g. ORU not ADT)
	ReasonUnsupportedTrigger RejectReason = "unsupported_trigger" // right class, unhandled trigger event (e.g. ADT^A40)
	ReasonMissingSegment     RejectReason = "missing_segment"     // required HL7 segment absent (e.g. no PID)
	ReasonMissingResource    RejectReason = "missing_resource"    // required FHIR resource absent (e.g. no Patient)
	ReasonAmbiguousSubject   RejectReason = "ambiguous_subject"   // more than one patient subject in one message
)

// RejectError categorizes a translation failure. Its Error() string is drawn
// ONLY from the reason category and an optional opaque fingerprint — it never
// interpolates payload content, so it is safe to surface in an audit trail.
type RejectError struct {
	Reason RejectReason
	// Fingerprint is an optional PHI-free correlation token (e.g. a hashed
	// case ID) when one could be recovered before the failure; empty on an
	// unrecoverable pre-parse error.
	Fingerprint string
}

func (e *RejectError) Error() string {
	if e.Fingerprint != "" {
		return "inbound rejected: " + string(e.Reason) + " [" + e.Fingerprint + "]"
	}
	return "inbound rejected: " + string(e.Reason)
}

// reject is a small constructor for the common no-fingerprint case.
func reject(r RejectReason) error { return &RejectError{Reason: r} }
