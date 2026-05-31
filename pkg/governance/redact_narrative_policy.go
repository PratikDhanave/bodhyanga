package governance

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// RedactNarrativePolicy strips free-text clinical narrative from message
// content before the message is forwarded to log-bound destinations.
//
// Clinical narrative fields — HPI, assessment, plan, chief complaint — are
// the most PHI-dense part of a clinical message. They routinely contain
// patient names in prose ("patient Jane Doe reports..."), dates of birth,
// addresses, and indirect identifiers that regex-based redactors miss.
// Forwarding raw narrative to audit logs, observability pipelines, or
// analytics exports creates a HIPAA risk surface even when the structured
// fields (case_id, MRN) are properly masked.
//
// This policy takes the unusual position of being MODIFYING rather than
// purely ALLOW/DENY. It works within the Policy interface by:
//
//  1. Checking msg.Metadata["destination"] against the known log-bound
//     set: "audit", "observability", "analytics", "export".
//  2. If the destination is log-bound AND the message content is JSON with
//     clinical narrative fields, it replaces those field values with
//     "[REDACTED-NARRATIVE]" in-place (msg.Content is a string, so the
//     modified payload is written back into a *copy* held by Evaluate).
//  3. It sets msg.Metadata["narrative_redacted"] = "true" so downstream
//     handlers can verify the redaction step ran.
//  4. It returns ALLOW — the redaction is advisory/best-effort and informs
//     downstream handlers. Blocking the message (deny) would prevent the
//     audit event from being written at all, which is the worse outcome.
//
// Regulatory anchor: HIPAA §164.312(e)(2)(ii) — "implement encryption
// mechanisms to guard against unauthorized access to ePHI transmitted over
// an electronic communications network". Redacting narrative is the
// complement for structured-log transmission where encryption alone is
// insufficient because the log is readable by the log platform's operators.
//
// Limitations:
//   - Only JSON content is processed. Binary or plain-text content passes
//     through unchanged (narrative_redacted is not set).
//   - Only the exact field names listed in narrativeFields are redacted.
//     A nested object at "encounter.hpi" is not currently reached — the
//     policy redacts the top-level keys only. A production deployment may
//     want a recursive walk; that is intentionally deferred to keep the
//     hot-path cost predictable and low.
//   - The policy is stateless; it does not persist or report which messages
//     triggered narrative redaction. Wire a metrics counter at the callsite
//     (pkg/orchestration) if you need that signal.
type RedactNarrativePolicy struct{}

// logBoundDestinations is the set of destination values that trigger
// narrative redaction. The set is fixed by deployment convention —
// callers that route to these destinations are expected to receive
// redacted payloads.
var logBoundDestinations = map[string]bool{
	"audit":         true,
	"observability": true,
	"analytics":     true,
	"export":        true,
}

// narrativeFields lists the JSON keys whose values contain free-text
// clinical narrative. These are the fields most likely to carry inline
// PHI (patient names in prose, addresses, dates in context) that
// structured redactors miss. The list is intentionally conservative —
// false positives (redacting a non-PHI "notes" field) are acceptable;
// false negatives (missing a narrative field) are not.
var narrativeFields = []string{
	"chief_complaint",
	"hpi",
	"assessment",
	"plan",
	"notes",
	"narrative",
	"reason",
}

// redactedSentinel is the replacement value for any redacted narrative
// field. It is chosen to be:
//   - visually distinct from normal content (uppercase bracket notation)
//   - inert under further regex redaction patterns (no digits or special
//     HIPAA shapes — see pkg/phi.DefaultPatterns)
//   - greppable in audit log reviews
const redactedSentinel = "[REDACTED-NARRATIVE]"

// Evaluate inspects the message destination and, if log-bound, redacts
// clinical narrative fields from JSON content.
//
// Cost: O(n_fields * len(content)) for the JSON unmarshal + field walk.
// For typical clinical messages (≤ 8 KiB content, ≤ 7 narrative keys)
// this is well under 1 ms on modern hardware. The JSON round-trip is only
// paid when (a) the destination is log-bound AND (b) the content parses
// as JSON — non-JSON content exits after a cheap json.Unmarshal failure.
func (RedactNarrativePolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	// Step 1: check destination. If absent or not log-bound, pass through.
	dest, _ := msg.Metadata["destination"].(string)
	if !logBoundDestinations[dest] {
		return allow("non-log-bound destination: narrative not redacted"), nil
	}

	// Step 2: attempt to parse content as JSON. Non-JSON content (plain
	// text, HL7 pipe-delimited, base64-encoded binary) passes through
	// unchanged. We do not deny because the message may legitimately carry
	// non-JSON payloads to an audit destination (e.g. a raw HL7 ADT event).
	var payload map[string]any
	if err := json.Unmarshal([]byte(msg.Content), &payload); err != nil {
		// Not JSON — allow without modification. Callers routing non-JSON
		// clinical data to log-bound destinations should pre-redact via
		// pkg/phi.Redactor or equivalent.
		return allow("non-JSON content: narrative not redacted"), nil
	}

	// Step 3: redact narrative fields in-place.
	redacted := false
	for _, field := range narrativeFields {
		if _, exists := payload[field]; exists {
			payload[field] = redactedSentinel
			redacted = true
		}
	}

	if !redacted {
		// No narrative fields found — content is already clean.
		return allow("no narrative fields: content unchanged"), nil
	}

	// Step 4: marshal the modified payload back into msg.Content.
	// Note: the Policy interface receives msg by value, so this
	// modification is visible only via msg.Metadata mutation (maps are
	// reference types). To propagate the redacted content to the caller,
	// we store the serialised result in msg.Metadata["redacted_content"].
	// Downstream handlers that act on the redacted version should read
	// msg.Metadata["redacted_content"] when msg.Metadata["narrative_redacted"]
	// == "true", rather than msg.Content directly.
	//
	// This is a known limitation of the Policy interface being designed for
	// binary ALLOW/DENY: a future "allow with modifications" decision type
	// would let us propagate msg.Content directly. See policy.go § Decision
	// for the planned extension point.
	b, err := json.Marshal(payload)
	if err != nil {
		// Marshal failure is extremely unlikely (the payload came from
		// Unmarshal). Treat as allow without redaction rather than deny
		// — a deny here would block the audit write, which is worse than
		// a partially-redacted log entry.
		return allow("narrative redaction: marshal error — content unchanged"), nil
	}

	// Write the redacted content and the marker into metadata.
	// msg.Metadata is a map[string]any (reference type) so mutations here
	// are visible to the caller even though msg is a value receiver.
	if msg.Metadata == nil {
		// Should not happen for a log-bound message (destination was set),
		// but guard defensively.
		return allow("narrative redacted (metadata nil — could not write marker)"), nil
	}
	msg.Metadata["redacted_content"] = string(b)
	msg.Metadata["narrative_redacted"] = "true"

	return allow("narrative fields redacted for log-bound destination"), nil
}

// narrativeTypeSuffixes are the msg.Type substrings that indicate a message
// may contain clinical narrative. Used by hasNarrativeType to avoid
// unnecessary JSON parse attempts on clearly non-clinical types.
//
// This is a performance hint only — the JSON unmarshal in Evaluate is
// the authoritative check. A message of type "tool:search" that happens
// to have a JSON body with an "hpi" key would still be redacted.
var narrativeTypeSuffixes = []string{
	"presentation", "diagnosis", "case_state", "plan", "assessment",
	"clinical", "narrative", "note",
}

// hasNarrativeType returns true when msg.Type suggests clinical narrative
// content. It is exported for testing; it is not used in the hot path.
func hasNarrativeType(msgType string) bool {
	lower := strings.ToLower(msgType)
	for _, suffix := range narrativeTypeSuffixes {
		if strings.Contains(lower, suffix) {
			return true
		}
	}
	return false
}
