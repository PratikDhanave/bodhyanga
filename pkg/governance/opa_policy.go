package governance

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
	"github.com/open-policy-agent/opa/v1/rego"
)

// regoMedical is the embedded Rego source for the medical governance rules
// (case_id, diagnostic cost, tenant). Embedded at compile time so the
// policy travels with the binary and operators can audit the exact source
// that produced a given deployment's decisions.
//
//go:embed rego/medical.rego
var regoMedical string

// OPAPolicy evaluates a pre-compiled OPA Rego rule against a protocol.Message
// to implement the Policy interface.
//
// The Rego module must define a rule at queryPath that evaluates to an object:
//
//	{
//	    "allowed": bool,
//	    "reason":  string
//	}
//
// The Reason string lands in audit.Event.Reason and is part of the public
// contract (see medical_policy.go allow/deny docstrings). Keep Reason values:
//   - specific and actionable ("case_id required for clinical messages")
//   - PHI-free (never echo msg.Content or patient identifiers)
//   - stable across versions (downstream SIEMs group/filter by Reason)
//
// # Performance
//
// The Rego module is compiled once in NewOPAPolicy and the resulting
// PreparedEvalQuery is goroutine-safe. Evaluate has no per-call compilation
// overhead, fitting the sub-100µs governance hot-path budget. OPA's
// in-process evaluation is synchronous and allocates modestly for small
// input documents (typical protocol.Message).
//
// # Fail-closed posture
//
// Any OPA evaluation error or unexpected result type produces a deny, not an
// allow. This matches the Policy contract (non-nil error = deny in
// CompositePolicy.Evaluate and in the orchestrator's policy-error path).
type OPAPolicy struct {
	query rego.PreparedEvalQuery
}

// NewOPAPolicy compiles module and prepares the evaluation query for queryPath.
//
// queryPath must be the full OPA rule path including the "data." prefix, e.g.
// "data.bodhyanga.governance.medical.case_id_result". The rule at that path must
// evaluate to an {"allowed": bool, "reason": string} object.
//
// Returns an error if the Rego source fails to parse or compile. A compile
// error from an embedded module (used by the domain factory functions) is
// always a programming error; use MustOPAPolicy in those cases.
func NewOPAPolicy(module, queryPath string) (*OPAPolicy, error) {
	q, err := rego.New(
		rego.Query(fmt.Sprintf("x = %s", queryPath)),
		rego.Module("governance.rego", module),
	).PrepareForEval(context.Background())
	if err != nil {
		return nil, fmt.Errorf("opa policy: compile %q: %w", queryPath, err)
	}
	return &OPAPolicy{query: q}, nil
}

// MustOPAPolicy is like NewOPAPolicy but panics on error.
//
// Use in package-level var blocks or init() where the Rego source is embedded
// at compile time and known to be valid — a compile failure there is always a
// programming error and should crash the process at startup rather than be
// silently mishandled.
func MustOPAPolicy(module, queryPath string) *OPAPolicy {
	p, err := NewOPAPolicy(module, queryPath)
	if err != nil {
		panic(fmt.Sprintf("opa policy: %v", err))
	}
	return p
}

// Evaluate runs the compiled Rego query against msg.
//
// The message is converted to an OPA input document (see messageToOPAInput).
// Fail-closed: any OPA error, empty result set, or unexpected result type
// produces a deny PolicyResult — consistent with the governance fail-closed
// posture (design rule 4: never silently admit a message on policy error).
func (p *OPAPolicy) Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error) {
	res, err := p.query.Eval(ctx, rego.EvalInput(messageToOPAInput(msg)))
	if err != nil {
		// Propagate as a Go error so CompositePolicy records it and
		// the orchestrator's policy-error path logs + denies the message.
		return PolicyResult{}, fmt.Errorf("opa policy: eval: %w", err)
	}
	if len(res) == 0 {
		// Empty result set: the rule is undefined for this input.
		// Deny (fail-closed) rather than silently allow.
		return deny("opa: rule produced no result"), nil
	}
	obj, ok := res[0].Bindings["x"].(map[string]any)
	if !ok {
		// Rule returned a non-object type. Misconfigured module — deny.
		return deny("opa: unexpected result type"), nil
	}
	allowed, _ := obj["allowed"].(bool)
	reason, _ := obj["reason"].(string)
	if reason == "" {
		reason = "opa policy"
	}
	if !allowed {
		return deny(reason), nil
	}
	return allow(reason), nil
}

// messageToOPAInput converts a protocol.Message to the OPA input document
// that governance Rego rules evaluate against.
//
// Field mapping:
//
//	input.id         — msg.ID (UUID, PHI-safe correlator)
//	input.from       — msg.From (sender identity)
//	input.to         — msg.To (recipient; empty string for broadcasts)
//	input.role       — msg.Role as string ("user", "agent", etc.)
//	input.type       — msg.Type (routing label, e.g. "presentation")
//	input.content    — msg.Content (full payload; rules may check length)
//	input.created_at — msg.CreatedAt in RFC3339 UTC
//	input.metadata   — deep copy of msg.Metadata, or JSON null when nil
//
// Nil vs empty metadata are preserved as distinct values: nil → JSON null,
// non-nil empty map → JSON {}. Rego rules check `input.metadata == null`
// to distinguish "caller omitted the metadata map entirely" (often a wiring
// bug) from "metadata present but key absent" (auth/gateway misconfiguration).
// This mirrors the Go policies' msg.Metadata == nil checks exactly.
//
// Content is included so content-length rules can apply count(input.content).
// PHI is not filtered at this layer; the Rego module is responsible for
// referencing only PHI-safe fields in Reason strings.
func messageToOPAInput(msg protocol.Message) map[string]any {
	// Preserve nil vs empty map: nil metadata serialises to JSON null so
	// Rego rules can distinguish the two cases with `input.metadata == null`.
	var md any
	if msg.Metadata != nil {
		m := make(map[string]any, len(msg.Metadata))
		for k, v := range msg.Metadata {
			m[k] = v
		}
		md = m
	}
	return map[string]any{
		"id":         msg.ID,
		"from":       msg.From,
		"to":         msg.To,
		"role":       string(msg.Role),
		"type":       msg.Type,
		"content":    msg.Content,
		"created_at": msg.CreatedAt.UTC().Format(time.RFC3339),
		"metadata":   md,
	}
}

// =============================================================================
// Domain factory functions — pre-wired for Bodhyanga's standard policies
// =============================================================================

// NewCaseIDOPAPolicy returns an OPA-backed Policy that denies clinical
// messages missing a non-empty case_id in metadata.
//
// Mirrors RequireCaseIDPolicy using the embedded medical.rego Rego source.
// Prefer the Go policy for maximum performance; use this when you need the
// rule to be independently auditable or swappable at deploy time.
func NewCaseIDOPAPolicy() (*OPAPolicy, error) {
	return NewOPAPolicy(regoMedical, "data.bodhyanga.governance.medical.case_id_result")
}

// NewDiagnosticCostOPAPolicy returns an OPA-backed Policy that denies
// order_test messages that would push spent + test cost over the per-case
// USD ceiling declared in metadata.
//
// Mirrors MaxDiagnosticCostPolicy using the embedded medical.rego.
func NewDiagnosticCostOPAPolicy() (*OPAPolicy, error) {
	return NewOPAPolicy(regoMedical, "data.bodhyanga.governance.medical.diagnostic_cost_result")
}

// NewTenantOPAPolicy returns an OPA-backed Policy that denies messages
// missing a non-empty tenant_id in metadata.
//
// Mirrors RequireTenantPolicy using the embedded medical.rego.
func NewTenantOPAPolicy() (*OPAPolicy, error) {
	return NewOPAPolicy(regoMedical, "data.bodhyanga.governance.medical.tenant_result")
}

// NewContentLengthOPAPolicy returns an OPA-backed Policy that denies messages
// whose content exceeds maxBytes Unicode code points.
//
// Mirrors MaxContentLengthPolicy. Note: OPA's count() on strings counts
// Unicode code points, not bytes. For ASCII clinical text the values are
// equivalent; multi-byte UTF-8 content may be slightly more permissive than
// the Go policy's len() check. Use the Go policy for byte-exact enforcement.
func NewContentLengthOPAPolicy(maxBytes int) (*OPAPolicy, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("opa content policy: maxBytes must be positive, got %d", maxBytes)
	}
	return NewOPAPolicy(buildContentLengthModule(maxBytes),
		"data.bodhyanga.governance.content.result")
}

// NewScopeOPAPolicy returns an OPA-backed Policy that enforces SMART-on-FHIR
// scope requirements. required maps message Type to the required scope string,
// e.g. {"cdm_request": "user/CarePlan.write"}.
//
// The scope requirements are baked into the compiled Rego module so the policy
// is self-contained and independently auditable — operators can inspect the
// generated source to verify exactly which scopes are required for each type.
//
// Mirrors RequireScopePolicy. scopes are read from msg.Metadata["scopes"]
// (space-separated OAuth 2.0 introspection format, same as the Go policy).
func NewScopeOPAPolicy(required map[string]string) (*OPAPolicy, error) {
	return NewOPAPolicy(buildScopeModule(required),
		"data.bodhyanga.governance.scope.result")
}

// =============================================================================
// Module builders — generate Rego source for parameterised policies
// =============================================================================

// buildContentLengthModule generates a Rego module that denies content
// exceeding maxBytes code points. The max is baked in as a literal so the
// policy is self-contained and auditable.
func buildContentLengthModule(maxBytes int) string {
	return fmt.Sprintf(`package bodhyanga.governance.content
import rego.v1

# max_codepoints is %d (baked in at policy construction time).
# Note: OPA count() counts Unicode code points, not bytes. For ASCII content
# the values are equivalent. See NewContentLengthOPAPolicy for details.
max_codepoints := %d

default result := {"allowed": false, "reason": "content too long"}

result := {"allowed": true, "reason": "within content length limit"} if {
    count(input.content) <= max_codepoints
}
`, maxBytes, maxBytes)
}

// buildScopeModule generates a Rego module with the required scope map baked
// in as a literal. The generated source is deterministic for a given required
// map (keys are sorted for reproducibility in tests).
func buildScopeModule(required map[string]string) string {
	var sb strings.Builder
	sb.WriteString(`package bodhyanga.governance.scope
import rego.v1

# required_scope maps message type -> SMART-on-FHIR scope string.
# Baked in at policy construction time; update via NewScopeOPAPolicy.
required_scope := {
`)
	// Sort keys for deterministic output.
	keys := make([]string, 0, len(required))
	for k := range required {
		keys = append(keys, k)
	}
	// Simple insertion sort — required map is small (< 20 entries).
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	for _, k := range keys {
		fmt.Fprintf(&sb, "    %q: %q,\n", k, required[k])
	}
	sb.WriteString(`}

default result := {"allowed": false, "reason": "required scope missing"}

# Type not gated by this policy — allow.
result := {"allowed": true, "reason": "no scope required for type"} if {
    not required_scope[input.type]
}

# Type gated, scope present in metadata — allow.
result := {"allowed": true, "reason": concat("", ["scope present: ", required_scope[input.type]])} if {
    req := required_scope[input.type]
    _has_scope(req)
}

# Type gated, scope absent — deny.
result := {"allowed": false, "reason": concat("", ["required scope missing: ", required_scope[input.type]])} if {
    req := required_scope[input.type]
    not _has_scope(req)
}

# _has_scope returns true iff req appears in the space-separated scopes string.
# Uses object.get for safety when metadata or the scopes key is absent.
_has_scope(req) if {
    metadata := object.get(input, "metadata", {})
    scopes_str := object.get(metadata, "scopes", "")
    some s in split(scopes_str, " ")
    s == req
}
`)
	return sb.String()
}
