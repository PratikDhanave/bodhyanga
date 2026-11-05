package governance

import (
	"context"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/auth"
	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
)

// SMARTScopePolicy is a governance.Policy that validates SMART on FHIR access
// tokens and enforces a required scope on every message that carries a
// jwt_token in its metadata.
//
// Usage in a CompositePolicy stack:
//
//	governance.NewComposite(
//	    governance.MaxContentLengthPolicy{Max: 65536},
//	    governance.NewSMARTScopePolicy(issuer, jwksURL, "patient/Observation.read"),
//	    governance.RequireCaseIDPolicy{},
//	)
//
// Token flow:
//  1. The API gateway validates the bearer token, then injects it into
//     msg.Metadata["jwt_token"] before publishing the message onto the bus.
//  2. SMARTScopePolicy validates the token against the SMART authorization
//     server's public keys (fetched from jwksURL; cached 5 min).
//  3. On success the policy allows the message and sets the patient context
//     in msg.Metadata["smart_patient"] so downstream agents can use it
//     without re-parsing the token.
//
// Regulatory anchor: SMART on FHIR scope semantics (HL7 SMART App Launch IG)
// and HIPAA §164.308(a)(4) information-access management. The scope assertion
// is the cryptographically-bound proof that the EHR authorization server
// granted this app access to the resource the message implies.
//
// Failure mode prevented: a misconfigured gateway forwarding messages without
// a valid token, or a token issued without the required scope (e.g. an app
// that only has offline_access trying to read Observation resources).
type SMARTScopePolicy struct {
	validator *auth.SMARTValidator
	required  string // required scope, e.g. "patient/Observation.read"
}

// NewSMARTScopePolicy constructs a policy that validates SMART tokens issued
// by issuer and requires requiredScope in the token's scope claim.
//
//   - issuer is the expected "iss" claim, e.g. "https://ehr.example.com".
//   - jwksURL is the JWKS URI exposed by the authorization server, e.g.
//     "https://ehr.example.com/.well-known/jwks.json".
//   - requiredScope is the SMART scope the caller must hold, e.g.
//     "patient/Observation.read" or "user/*.read".
func NewSMARTScopePolicy(issuer, jwksURL, requiredScope string) *SMARTScopePolicy {
	return &SMARTScopePolicy{
		validator: auth.NewSMARTValidator(issuer, jwksURL),
		required:  requiredScope,
	}
}

// Evaluate validates the SMART token in msg.Metadata["jwt_token"] and checks
// that the required scope is present.
//
// Decision matrix:
//   - No jwt_token in metadata → DENY ("not authenticated")
//   - Token present but invalid (expired, bad sig, wrong iss) → DENY ("invalid token: …")
//   - Token valid but scope missing → DENY ("required scope missing: …")
//   - Token valid and scope present → ALLOW; patient context written to metadata
//
// PHI note: deny reasons are intentionally short and contain no patient data.
// The token string itself is never echoed into the audit log.
func (p *SMARTScopePolicy) Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error) {
	// Guard: metadata map must exist and contain jwt_token.
	if msg.Metadata == nil {
		return PolicyResult{
			Decision:  DecisionDeny,
			Reason:    "not authenticated: no metadata",
			CheckedAt: time.Now().UTC(),
		}, nil
	}
	rawToken, ok := msg.Metadata["jwt_token"].(string)
	if !ok || rawToken == "" {
		return PolicyResult{
			Decision:  DecisionDeny,
			Reason:    "not authenticated: no jwt_token",
			CheckedAt: time.Now().UTC(),
		}, nil
	}

	// Validate the SMART token.
	claims, err := p.validator.Validate(ctx, rawToken)
	if err != nil {
		// err is safe to log: it contains only validation diagnostics.
		return PolicyResult{
			Decision:  DecisionDeny,
			Reason:    "invalid token: " + err.Error(),
			CheckedAt: time.Now().UTC(),
		}, nil
	}

	// Check the required scope.
	if !p.validator.HasScope(claims, p.required) {
		return PolicyResult{
			Decision:  DecisionDeny,
			Reason:    "required scope missing: " + p.required,
			CheckedAt: time.Now().UTC(),
		}, nil
	}

	// All checks passed. Propagate the patient context so downstream agents
	// can use it without re-parsing the token. Using the key "smart_patient"
	// (not "patient_id") keeps it distinguishable from ADT-sourced patient IDs
	// in the metadata map.
	if claims.Patient != "" && msg.Metadata != nil {
		msg.Metadata["smart_patient"] = claims.Patient
	}

	return PolicyResult{
		Decision:  DecisionAllow,
		Reason:    "smart scope " + p.required + " present",
		CheckedAt: time.Now().UTC(),
	}, nil
}
