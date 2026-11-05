package governance

import (
	"context"
	"strings"

	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
)

// Part2PolicyName is the canonical identifier for the 42 CFR Part 2 consent policy.
// It is reserved for use in PolicyResult.CheckedByID and audit dashboards.
const Part2PolicyName = "42CFRPart2ConsentPolicy"

// sudMarkers is the set of condition strings that identify a message as carrying
// Substance Use Disorder (SUD) record information subject to 42 CFR Part 2.
//
// The list covers both diagnostic terms (opioid_use_disorder, alcohol_use_disorder)
// and treatment/medication markers (methadone, buprenorphine, naltrexone) because
// Part 2 protection follows the record regardless of whether the condition appears
// as a diagnosis code or as an implied condition in a medication order. Substring
// matching (via strings.Contains) means a value like "opioid_use_disorder_severe"
// is correctly identified as SUD-bearing.
var sudMarkers = []string{
	"opioid_use_disorder",
	"alcohol_use_disorder",
	"substance_use",
	"sud",
	"opioid",
	"methadone",
	"buprenorphine",
	"naltrexone",
	"addiction",
}

// Part2ConsentPolicy enforces 42 CFR Part 2 — Substance Use Disorder (SUD)
// record protection.
//
// Regulatory anchor: 42 CFR Part 2 (Confidentiality of Substance Use Disorder
// Patient Records) prohibits disclosure of SUD treatment records without patient
// consent, even for treatment purposes — this is stricter than HIPAA's
// treatment-payment-operations exception. Any message carrying SUD condition
// markers must have explicit patient consent on file (part2_consent=true in
// metadata) or a documented emergency break-glass override (break_glass=true)
// to proceed.
//
// Failure mode prevented: SUD records reaching a downstream agent (diagnostician,
// care-plan generator, researcher) without the patient's written consent. An
// audit of a Part 2 violation could result in loss of federal funding for the
// covered program, in addition to civil liability.
//
// Decision logic:
//  1. If msg.Metadata["condition"] does NOT contain a SUD marker → ALLOW
//     (non-SUD records are not subject to Part 2 and flow through unimpeded)
//  2. If msg.Metadata["part2_consent"] == "true" → ALLOW (consent on file)
//  3. If msg.Metadata["break_glass"] == "true" → ALLOW (emergency override)
//  4. Otherwise → DENY with a reason citing the regulation
//
// PHI safety: the Reason string is PHI-free. The deny message names the
// regulation and the missing metadata key without echoing any patient data.
//
// Example deny audit record (illustrative):
//
//	{kind: "policy_decision", decision: "deny",
//	 reason: "42 CFR Part 2: SUD record access requires patient consent (part2_consent=true)",
//	 type: "diagnosis"}
type Part2ConsentPolicy struct{}

// Evaluate enforces the Part 2 consent gate.
//
// Cost: O(n_markers * len(condition)) — typically O(1) in practice because
// both the marker list (9 strings) and condition values are short. No
// allocations in the allow-without-SUD fast path.
func (Part2ConsentPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	// --- Step 1: Check whether the message carries SUD markers. ---
	// An absent or non-string condition is treated as "no SUD markers present"
	// (not as a deny), consistent with the fail-open principle for non-SUD
	// data: we must not block legitimate non-SUD clinical traffic just because
	// the condition field is absent. RequireCaseIDPolicy handles missing
	// metadata for clinical types; Part 2 only governs when SUD data is
	// explicitly present.
	condition := ""
	if msg.Metadata != nil {
		if v, ok := msg.Metadata["condition"].(string); ok {
			condition = strings.ToLower(v)
		}
	}

	if !hasSUDMarker(condition) {
		// No SUD condition detected — Part 2 does not apply to this message.
		return allow("no SUD condition detected, Part 2 not applicable"), nil
	}

	// --- Step 2: SUD markers present. Check consent and break-glass. ---

	// Consent on file: part2_consent="true" in metadata satisfies Part 2.
	// The string "true" is used (not bool) to match the Metadata map[string]any
	// convention used by the existing policies (e.g., msg.Metadata["part2_consent"]).
	if msg.Metadata != nil {
		if v, ok := msg.Metadata["part2_consent"].(string); ok && v == "true" {
			return allow("Part 2 consent on file"), nil
		}

		// Break-glass emergency override: allows access without consent in
		// a documented emergency. Must be audited separately by the
		// break-glass workflow (see docs/governance.md § BreakGlass).
		if v, ok := msg.Metadata["break_glass"].(string); ok && v == "true" {
			return allow("Part 2 break-glass emergency override"), nil
		}
	}

	// --- Step 3: SUD record, no consent, no break-glass — DENY. ---
	return deny("42 CFR Part 2: SUD record access requires patient consent (part2_consent=true)"), nil
}

// hasSUDMarker returns true if the (lowercased) condition string contains
// any of the known SUD markers as a substring.
//
// Substring matching is intentional: it catches compound values such as
// "severe_opioid_use_disorder" or "opioid_use_disorder_moderate" without
// requiring callers to normalise to an exact enum.
func hasSUDMarker(condition string) bool {
	if condition == "" {
		return false
	}
	for _, marker := range sudMarkers {
		if strings.Contains(condition, marker) {
			return true
		}
	}
	return false
}
