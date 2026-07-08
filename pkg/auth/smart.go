// Package auth — SMART on FHIR extension.
//
// SMARTValidator validates RS256 JWT access tokens issued by a SMART on FHIR
// authorization server. It fetches the server's JWKS on first use, caches the
// key set for five minutes, and verifies the JWT signature, issuer, and
// expiration. The parsed claims are returned as *SMARTClaims.
//
// ParseScopes and BuildSMARTScopes are convenience helpers for working with
// OAuth 2.0 scope strings in the SMART on FHIR vocabulary.
//
// See https://smarthealthit.org/ and HL7 SMART App Launch IG for the full
// specification.
package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SMARTScopes is a structured view of a SMART on FHIR scope string.
//
// SMART scopes follow the pattern context/ResourceType.action, where context
// is one of "patient", "user", or "system". Launch scopes ("launch",
// "launch/patient", "launch/encounter") and OpenID scopes ("openid",
// "fhirUser", "profile") are kept in their own slices for fast membership
// checks without string splitting at each call site.
type SMARTScopes struct {
	// Patient holds patient-context resource scopes, e.g.
	// "patient/Observation.read", "patient/*.read".
	Patient []string
	// User holds user-context resource scopes, e.g. "user/Patient.read".
	User []string
	// System holds system-context resource scopes, e.g. "system/Patient.read".
	System []string
	// Launch holds launch context scopes, e.g. "launch/patient",
	// "launch/encounter", "launch".
	Launch []string
	// OpenID holds identity scopes: "openid", "fhirUser", "profile".
	OpenID []string
}

// SMARTClaims carries the SMART on FHIR JWT claim set.
//
// The struct is intentionally flat. Sub, FHIRUser, Patient, and Encounter
// provide the identity + context needed by clinical policies; Scope is the raw
// space-separated OAuth 2.0 scope string; ClientID identifies the app; Iss /
// Exp / Iat are standard JWT claims used during validation.
type SMARTClaims struct {
	// Sub is the patient or user identifier (JWT "sub" claim).
	Sub string `json:"sub"`
	// FHIRUser is the FHIR URL for the authenticated clinician, e.g.
	// "https://ehr.example.com/fhir/Practitioner/123". Present for
	// user-launch flows; absent for patient-facing apps.
	FHIRUser string `json:"fhirUser,omitempty"`
	// Patient is the FHIR patient context ID injected by the EHR during
	// launch. Non-empty when the token was issued with launch/patient.
	Patient string `json:"patient,omitempty"`
	// Encounter is the FHIR encounter context ID (launch/encounter).
	Encounter string `json:"encounter,omitempty"`
	// Scope is the raw space-separated OAuth 2.0 scope string as issued
	// by the authorization server.
	Scope string `json:"scope"`
	// ClientID is the OAuth 2.0 client identifier of the requesting app.
	ClientID string `json:"client_id"`
	// Iss is the token issuer. Validated against SMARTValidator.issuer.
	Iss string `json:"iss"`
	// Exp is the Unix timestamp when the token expires.
	Exp int64 `json:"exp"`
	// Iat is the Unix timestamp when the token was issued.
	Iat int64 `json:"iat"`
}

// jwks is the minimal JSON Web Key Set structure returned by the SMART
// authorization server's JWKS URI.
type jwks struct {
	Keys []jwk `json:"keys"`
}

// jwk is a single JSON Web Key. We only need the fields required for RSA
// signature verification (n, e, kid, kty, alg, use).
type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"` // Base64url-encoded modulus.
	E   string `json:"e"` // Base64url-encoded public exponent.
}

// jwksMaxAge is the cache TTL for the JWKS response. Five minutes balances
// key-rotation latency against inbound connection volume on high-throughput
// installations.
const jwksMaxAge = 5 * time.Minute

// SMARTValidator validates SMART on FHIR access tokens.
//
// Construct via NewSMARTValidator. The zero value is not usable.
//
// Thread safety: Validate, HasScope, and the cache refresh path are safe for
// concurrent use.
type SMARTValidator struct {
	issuer     string
	jwksURL    string
	httpClient *http.Client

	// keysMu guards keys and keysAt.
	keysMu sync.RWMutex
	// keys maps kid → DER-encoded RSA public key bytes. The bytes are
	// passed to x509.ParsePKIXPublicKey on each Validate call so the
	// *rsa.PublicKey lifetime is bounded to a single token verification.
	keys   map[string][]byte
	keysAt time.Time
}

// NewSMARTValidator constructs a validator for the supplied issuer + JWKS URI.
// A default HTTP client with a 10-second timeout is used; override via
// v.httpClient after construction if you need a custom transport.
func NewSMARTValidator(issuer, jwksURL string) *SMARTValidator {
	return &SMARTValidator{
		issuer:     issuer,
		jwksURL:    jwksURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		keys:       make(map[string][]byte),
	}
}

// Validate parses and verifies a raw JWT access token.
//
// Validation steps:
//  1. Base64url-decode and JSON-unmarshal the header and payload.
//  2. Fetch (or use cached) JWKS keys from jwksURL.
//  3. Look up the key by kid from the JWT header.
//  4. Verify the RS256 signature.
//  5. Check exp against the current clock.
//  6. Check iss against v.issuer.
//
// Returns a pointer to the parsed SMARTClaims on success. Returns a non-nil
// error for any failure; the error is safe to log (no PHI).
func (v *SMARTValidator) Validate(ctx context.Context, token string) (*SMARTClaims, error) {
	// Split into header.payload.signature parts.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("smart: malformed JWT (expected 3 parts)")
	}

	// Decode + parse the header to extract kid and alg.
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("smart: decode header: %w", err)
	}
	var header struct {
		Kid string `json:"kid"`
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("smart: parse header: %w", err)
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("smart: unsupported alg %q (only RS256 accepted)", header.Alg)
	}

	// Decode + parse the payload to get claims.
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("smart: decode payload: %w", err)
	}
	var claims SMARTClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("smart: parse claims: %w", err)
	}

	// Fetch JWKS (with 5-minute caching).
	if err := v.ensureKeys(ctx); err != nil {
		return nil, fmt.Errorf("smart: fetch JWKS: %w", err)
	}

	// Look up the signing key.
	v.keysMu.RLock()
	keyDER, ok := v.keys[header.Kid]
	v.keysMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("smart: unknown kid %q", header.Kid)
	}

	// Parse the DER bytes into an RSA public key.
	pub, err := x509.ParsePKIXPublicKey(keyDER)
	if err != nil {
		return nil, fmt.Errorf("smart: parse public key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("smart: key is not an RSA public key")
	}

	// Verify the RS256 signature over header.payload.
	signingInput := parts[0] + "." + parts[1]
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("smart: decode signature: %w", err)
	}
	if err := verifyRS256([]byte(signingInput), sigBytes, rsaPub); err != nil {
		return nil, fmt.Errorf("smart: signature verification failed: %w", err)
	}

	// Validate exp.
	if claims.Exp == 0 {
		return nil, errors.New("smart: token has no exp claim")
	}
	if time.Now().Unix() > claims.Exp {
		return nil, errors.New("smart: token is expired")
	}

	// Validate iss.
	if claims.Iss != v.issuer {
		return nil, fmt.Errorf("smart: issuer mismatch: got %q, want %q", claims.Iss, v.issuer)
	}

	return &claims, nil
}

// HasScope reports whether the required scope is present in claims.Scope.
// The scope string is space-separated per OAuth 2.0 convention.
func (v *SMARTValidator) HasScope(claims *SMARTClaims, required string) bool {
	if claims == nil {
		return false
	}
	for _, s := range strings.Fields(claims.Scope) {
		if s == required {
			return true
		}
	}
	return false
}

// ensureKeys fetches the JWKS if the cache is empty or older than jwksMaxAge.
// It is safe for concurrent callers; the fetch is serialized by the write lock.
func (v *SMARTValidator) ensureKeys(ctx context.Context) error {
	// Fast path: read lock only when cache is fresh.
	v.keysMu.RLock()
	fresh := !v.keysAt.IsZero() && time.Since(v.keysAt) < jwksMaxAge
	v.keysMu.RUnlock()
	if fresh {
		return nil
	}

	// Slow path: acquire write lock and re-check (another goroutine may have
	// refreshed the cache while we were waiting).
	v.keysMu.Lock()
	defer v.keysMu.Unlock()
	if !v.keysAt.IsZero() && time.Since(v.keysAt) < jwksMaxAge {
		return nil // refreshed by another goroutine.
	}
	return v.fetchKeys(ctx)
}

// fetchKeys fetches the JWKS from jwksURL and populates v.keys.
// Must be called with v.keysMu held for writing.
func (v *SMARTValidator) fetchKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", v.jwksURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned HTTP %d", resp.StatusCode)
	}

	var set jwks
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}

	newKeys := make(map[string][]byte, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue // only RSA keys are used for RS256.
		}
		der, err := jwkRSAToDER(k)
		if err != nil {
			// Skip undecodable keys; they might be for other algorithms.
			continue
		}
		newKeys[k.Kid] = der
	}
	v.keys = newKeys
	v.keysAt = time.Now()
	return nil
}

// jwkRSAToDER converts a JWK RSA key to PKIX DER bytes suitable for
// x509.ParsePKIXPublicKey. Returns an error if n or e is invalid.
func jwkRSAToDER(k jwk) ([]byte, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}
	if len(eBytes) > 4 {
		return nil, errors.New("public exponent too large")
	}
	// Pad eBytes to 4 bytes (big-endian).
	var eBuf [4]byte
	copy(eBuf[4-len(eBytes):], eBytes)
	e := int(eBuf[0])<<24 | int(eBuf[1])<<16 | int(eBuf[2])<<8 | int(eBuf[3])
	pub := &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: e,
	}
	return x509.MarshalPKIXPublicKey(pub)
}

// verifyRS256 verifies an RS256 signature over the signing input (ASCII bytes).
// It uses crypto/rsa.VerifyPKCS1v15 with SHA-256.
func verifyRS256(signingInput, sig []byte, pub *rsa.PublicKey) error {
	h := sha256.Sum256(signingInput)
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], sig)
}

// ParseScopes splits a space-separated SMART scope string into a SMARTScopes
// struct. Each scope token is classified by its prefix:
//
//   - "patient/" → Patient
//   - "user/" → User
//   - "system/" → System
//   - "launch" (or "launch/…") → Launch
//   - "openid", "fhirUser", "profile" → OpenID
func ParseScopes(scopeStr string) SMARTScopes {
	var s SMARTScopes
	for _, tok := range strings.Fields(scopeStr) {
		switch {
		case strings.HasPrefix(tok, "patient/"):
			s.Patient = append(s.Patient, tok)
		case strings.HasPrefix(tok, "user/"):
			s.User = append(s.User, tok)
		case strings.HasPrefix(tok, "system/"):
			s.System = append(s.System, tok)
		case tok == "launch" || strings.HasPrefix(tok, "launch/"):
			s.Launch = append(s.Launch, tok)
		case tok == "openid" || tok == "fhirUser" || tok == "profile":
			s.OpenID = append(s.OpenID, tok)
		}
	}
	return s
}

// BuildSMARTScopes constructs a single SMART scope token from its components.
//
// context must be one of "patient", "user", or "system".
// resource is the FHIR resource type (e.g. "Observation", "Patient", "*").
// action is "read", "write", or "*".
//
// Example:
//
//	BuildSMARTScopes("Observation", "read", "patient") // → "patient/Observation.read"
func BuildSMARTScopes(resource, action, context string) string {
	return context + "/" + resource + "." + action
}
