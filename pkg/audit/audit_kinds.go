package audit

// KindBreakGlass is emitted whenever a clinician invokes the emergency
// break-glass override to access records they would not normally be
// permitted to see (e.g. an ED physician accessing a patient not in their
// panel). Every break-glass access is unconditionally audited regardless
// of other audit filters — this is the HIPAA §164.312(b) anchor for
// emergency-access events and the trail required by most covered-entity
// policies under §164.308(a)(1)(ii)(D) information-system activity review.
//
// The event carries:
//   - AgentID: "orchestrator" (policy evaluation boundary, same as KindPolicyDecision)
//   - MessageID / From / To / Type: routing envelope from the message
//   - Decision: "allow" (break-glass that passes validation is still allowed)
//   - Reason: "break_glass access granted" (PHI-free confirmation string)
//   - PatientID / CaseID / TenantID / TraceID: correlation handles from metadata
//
// The clinician who triggered the override is identified by the
// break_glass_clinician metadata key, which is copied into the Reason or
// captured by a downstream SIEM join on the audit record's MessageID. Raw
// clinician identifiers (e.g. NPI) are NOT stored here — the audit record
// points to the message envelope from which they can be retrieved under
// separate access control.
const KindBreakGlass Kind = "break_glass_access"
