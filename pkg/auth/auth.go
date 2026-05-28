// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/auth/auth.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodh:
//   - Claims carries tenant_id and roles for Bodh's multi-tenant + RBAC model.
//   - jwt/v5 instead of jwt/v4 (upstream still uses v4 at time of adaptation).
//   - No userbus dependency; user-credential flow is out of scope for Bodh.
//   - Rego package namespace is bodh.auth (not ardan.rego).
//   - Authenticator is the type name (not Auth) for clarity at call sites.

// Package auth provides JWT authentication (RS256) and OPA Rego-based
// authorization for Bodh. It is the foundation under cmd/care's HTTP
// surface when BODH_AUTH=jwt.
//
// Authentication: an Authenticator parses a Bearer token, looks up the
// public key by the JWT header's `kid`, and asks the OPA engine to
// verify the signature + issuer against the embedded authentication
// Rego policy. Claims (subject, tenant_id, roles, exp) are returned
// on success.
//
// Authorization: a separate Evaluate call runs a named Rego rule (e.g.
// "bodh.explain.allow") against a structured input (roles, tenant, method,
// path). The middleware in pkg/mid wraps this in a stdlib HTTP middleware.
//
// Key management: KeyLookup is a tiny interface satisfied by *KeyStore in
// this package. Keys are RSA 2048+ PEM-encoded; the keystore loads from a
// filesystem (one .pem file per kid) or from an in-memory JSON document.
//
// Bodh does not manage user credentials. The Authenticator only verifies
// tokens; issuing tokens is the operator's responsibility (the dev path
// uses cmd/bodhadmin to mint a token signed by a generated key pair).
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/open-policy-agent/opa/v1/rego"
)

// Sentinel errors returned by the Authenticator. Callers / middleware map
// these to HTTP 401 / 403 in pkg/mid.
var (
	// ErrMissingBearer is returned when the Authorization header is empty
	// or does not begin with "Bearer ". Middleware maps -> 401.
	ErrMissingBearer = errors.New("authorization header missing or not a Bearer token")
	// ErrKIDMissing is returned when the JWT header has no kid. Middleware
	// maps -> 401. A token without a kid is not unverifiable to a
	// multi-key keystore.
	ErrKIDMissing = errors.New("kid missing from token header")
	// ErrKIDMalformed is returned when the kid header is not a string.
	ErrKIDMalformed = errors.New("kid in token header is malformed")
	// ErrInvalidToken is returned when OPA's signature-verify policy
	// reports the token did not satisfy the rule. Middleware -> 401.
	ErrInvalidToken = errors.New("invalid token")
	// ErrForbidden is returned when an authorization rule evaluates to
	// false. Middleware -> 403.
	ErrForbidden = errors.New("forbidden: authorization rule denied")
)

// Claims is the Bodh JWT claim set. It embeds the registered JWT claims
// (iss, sub, aud, exp, iat, nbf, jti) plus two Bodh-specific fields:
//
//   - TenantID is the tenant scope the subject belongs to. Bodh policies
//     compare this against the resource's tenant for the "same tenant"
//     check. Single-tenant binaries derive ResourceTenantID from
//     BODH_TENANT_ID at boot.
//   - Roles is the array of role strings (e.g. "clinician", "auditor").
//     The Rego policies compare these against required role sets.
//
// The role catalogue is documented in docs/auth.md.
type Claims struct {
	jwt.RegisteredClaims
	TenantID string   `json:"tenant_id"`
	Roles    []string `json:"roles"`
}

// KeyLookup is the keystore interface the Authenticator uses to find the
// PEM-encoded RSA key for a given kid. Implemented by *KeyStore in this
// package; satisfiable by any external keystore (e.g. AWS KMS adapter).
type KeyLookup interface {
	PrivateKey(kid string) (string, error)
	PublicKey(kid string) (string, error)
}

// Config is the Authenticator constructor's input.
type Config struct {
	// KeyLookup resolves kid -> PEM. Required.
	KeyLookup KeyLookup
	// Issuer is the expected `iss` claim. Required and verified by the
	// authentication Rego policy.
	Issuer string
}

// Authenticator parses and validates JWTs and evaluates OPA Rego policies
// for authorization. The zero value is not usable — construct via New.
type Authenticator struct {
	keys   KeyLookup
	method jwt.SigningMethod
	parser *jwt.Parser
	issuer string
}

// New constructs an Authenticator with the supplied keystore + issuer.
// Signing method is pinned to RS256 — the upstream "alg=none" attack
// class is closed at the parser layer in addition to the Rego policy.
func New(cfg Config) *Authenticator {
	return &Authenticator{
		keys:   cfg.KeyLookup,
		method: jwt.SigningMethodRS256,
		parser: jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()})),
		issuer: cfg.Issuer,
	}
}

// Issuer returns the configured issuer string. Useful for test helpers
// that mint tokens (they need the iss to match this Authenticator).
func (a *Authenticator) Issuer() string { return a.issuer }

// GenerateToken signs the supplied claims with the private key for kid.
// Used by cmd/bodhadmin and by tests; production Bodh deployments would
// have an external issuer (the demo issues its own tokens for self-hosted
// runs).
func (a *Authenticator) GenerateToken(kid string, claims Claims) (string, error) {
	token := jwt.NewWithClaims(a.method, claims)
	token.Header["kid"] = kid

	privPEM, err := a.keys.PrivateKey(kid)
	if err != nil {
		return "", fmt.Errorf("private key for kid %q: %w", kid, err)
	}
	privKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privPEM))
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	signed, err := token.SignedString(privKey)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// Parse validates a Bearer token and returns its Claims. Returns
// ErrMissingBearer, ErrKIDMissing, ErrKIDMalformed, or ErrInvalidToken
// on failure. The token's signature is verified by an OPA Rego rule
// (defence-in-depth in addition to jwt/v5's own verification path).
func (a *Authenticator) Parse(ctx context.Context, bearer string) (Claims, error) {
	if !strings.HasPrefix(bearer, "Bearer ") {
		return Claims{}, ErrMissingBearer
	}
	raw := strings.TrimPrefix(bearer, "Bearer ")

	// ParseUnverified to extract the kid + claims; signature verification
	// happens via OPA + the kid-resolved public key. Upstream uses the
	// same pattern; the kid lookup needs the parsed header.
	var claims Claims
	tok, _, err := a.parser.ParseUnverified(raw, &claims)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	kidRaw, ok := tok.Header["kid"]
	if !ok {
		return Claims{}, ErrKIDMissing
	}
	kid, ok := kidRaw.(string)
	if !ok {
		return Claims{}, ErrKIDMalformed
	}

	pubPEM, err := a.keys.PublicKey(kid)
	if err != nil {
		return Claims{}, fmt.Errorf("public key for kid %q: %w", kid, err)
	}

	// Signature + issuer check via Rego (see pkg/auth/rego/authentication.rego).
	input := map[string]any{
		"Token": raw,
		"Key":   pubPEM,
		"ISS":   a.issuer,
	}
	allowed, err := evalRule(ctx, regoAuthentication, "data.bodh.auth.auth", input)
	if err != nil {
		return Claims{}, fmt.Errorf("authn rego: %w", err)
	}
	if !allowed {
		return Claims{}, ErrInvalidToken
	}

	// Belt-and-braces: jwt/v5 will check exp/nbf on its own when we Parse
	// with the keyfunc. Run that pass too so an expired token is caught
	// even if the Rego policy's exp handling silently changes.
	_, err = a.parser.ParseWithClaims(raw, &Claims{}, func(t *jwt.Token) (any, error) {
		return jwt.ParseRSAPublicKeyFromPEM([]byte(pubPEM))
	})
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	return claims, nil
}

// Evaluate runs a named Rego rule against the supplied input. Returns
// (true, nil) iff the rule's bound variable resolves to boolean true.
// Returns ErrForbidden on rule-denied so middleware can map to HTTP 403.
//
// query is the full path including the data prefix
// (e.g. "data.bodh.explain.allow"); module is the embedded Rego source.
func (a *Authenticator) Evaluate(ctx context.Context, module, query string, input map[string]any) error {
	allowed, err := evalRule(ctx, module, query, input)
	if err != nil {
		return fmt.Errorf("authz rego: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// evalRule prepares + evaluates a single boolean Rego query. Factored out
// of Parse and Evaluate so both paths share one OPA configuration.
func evalRule(ctx context.Context, module, query string, input map[string]any) (bool, error) {
	q, err := rego.New(
		rego.Query(fmt.Sprintf("x = %s", query)),
		rego.Module("policy.rego", module),
	).PrepareForEval(ctx)
	if err != nil {
		return false, fmt.Errorf("prepare: %w", err)
	}
	res, err := q.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return false, fmt.Errorf("eval: %w", err)
	}
	if len(res) == 0 {
		return false, nil
	}
	v, ok := res[0].Bindings["x"].(bool)
	return ok && v, nil
}
