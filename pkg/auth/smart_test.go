package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ─── test helpers ────────────────────────────────────────────────────────────

// testKey holds a freshly-generated RSA key pair and a pre-computed JWKS
// document served by the mock JWKS endpoint.
type testKey struct {
	priv    *rsa.PrivateKey
	kid     string
	jwksDoc []byte // JSON bytes ready to serve
}

// newTestKey generates a 2048-bit RSA key pair and builds the minimal JWKS
// document the validator's fetchKeys path will parse.
func newTestKey(t *testing.T, kid string) *testKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	tk := &testKey{priv: priv, kid: kid}
	tk.jwksDoc = mustMarshal(t, map[string]any{
		"keys": []map[string]any{tk.jwk()},
	})
	return tk
}

// jwk returns the JWK representation of the public key.
func (tk *testKey) jwk() map[string]any {
	pub := &tk.priv.PublicKey
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	// Encode E as a big-endian byte slice (minimal length, no leading zeros).
	eBig := new(big.Int).SetInt64(int64(pub.E))
	e := base64.RawURLEncoding.EncodeToString(eBig.Bytes())
	return map[string]any{
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"kid": tk.kid,
		"n":   n,
		"e":   e,
	}
}

// sign produces a raw RS256 JWT signed by tk.priv with the given payload
// claims merged on top of the defaults (iss, iat, exp).
func (tk *testKey) sign(t *testing.T, issuer string, extra map[string]any) string {
	t.Helper()
	header := map[string]any{
		"alg": "RS256",
		"typ": "JWT",
		"kid": tk.kid,
	}
	now := time.Now()
	payload := map[string]any{
		"sub":       "user-001",
		"iss":       issuer,
		"iat":       now.Unix(),
		"exp":       now.Add(time.Hour).Unix(),
		"scope":     "openid",
		"client_id": "test-app",
	}
	for k, v := range extra {
		payload[k] = v
	}
	hBytes := base64.RawURLEncoding.EncodeToString(mustMarshal(t, header))
	pBytes := base64.RawURLEncoding.EncodeToString(mustMarshal(t, payload))
	signingInput := hBytes + "." + pBytes

	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, tk.priv, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// wrongKeySign produces a JWT signed by a different RSA key so signature
// verification must fail.
func wrongKeySign(t *testing.T, issuer string, tk *testKey) string {
	t.Helper()
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}
	header := map[string]any{
		"alg": "RS256",
		"typ": "JWT",
		"kid": tk.kid, // correct kid, wrong key material
	}
	now := time.Now()
	payload := map[string]any{
		"sub":       "attacker",
		"iss":       issuer,
		"iat":       now.Unix(),
		"exp":       now.Add(time.Hour).Unix(),
		"scope":     "patient/*.read",
		"client_id": "evil-app",
	}
	hBytes := base64.RawURLEncoding.EncodeToString(mustMarshal(t, header))
	pBytes := base64.RawURLEncoding.EncodeToString(mustMarshal(t, payload))
	signingInput := hBytes + "." + pBytes

	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, other, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("sign with wrong key: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// mockJWKSServer starts an httptest.Server that serves tk.jwksDoc as JSON.
func mockJWKSServer(t *testing.T, tk *testKey) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(tk.jwksDoc)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// ─── Validate tests ───────────────────────────────────────────────────────────

// TestSMARTValidate_ValidToken checks the happy path: a well-formed,
// unexpired, correctly-signed RS256 JWT issued by the expected issuer.
func TestSMARTValidate_ValidToken(t *testing.T) {
	tk := newTestKey(t, "key-001")
	jwksSrv := mockJWKSServer(t, tk)

	const issuer = "https://smart.example.com"
	validator := NewSMARTValidator(issuer, jwksSrv.URL)

	token := tk.sign(t, issuer, map[string]any{
		"scope":    "openid patient/Observation.read",
		"patient":  "patient-42",
		"fhirUser": "https://smart.example.com/fhir/Practitioner/7",
	})

	claims, err := validator.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	if claims.Sub != "user-001" {
		t.Errorf("sub = %q, want %q", claims.Sub, "user-001")
	}
	if claims.Patient != "patient-42" {
		t.Errorf("patient = %q, want %q", claims.Patient, "patient-42")
	}
	if claims.FHIRUser != "https://smart.example.com/fhir/Practitioner/7" {
		t.Errorf("fhirUser = %q", claims.FHIRUser)
	}
	if claims.Iss != issuer {
		t.Errorf("iss = %q, want %q", claims.Iss, issuer)
	}
}

// TestSMARTValidate_ExpiredToken ensures an expired token is rejected.
func TestSMARTValidate_ExpiredToken(t *testing.T) {
	tk := newTestKey(t, "key-002")
	jwksSrv := mockJWKSServer(t, tk)

	const issuer = "https://smart.example.com"
	validator := NewSMARTValidator(issuer, jwksSrv.URL)

	// Build a token with exp already in the past.
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": tk.kid}
	payload := map[string]any{
		"sub":       "user-x",
		"iss":       issuer,
		"iat":       time.Now().Add(-2 * time.Hour).Unix(),
		"exp":       time.Now().Add(-1 * time.Hour).Unix(), // expired
		"scope":     "openid",
		"client_id": "test-app",
	}
	hBytes := base64.RawURLEncoding.EncodeToString(mustMarshal(t, header))
	pBytes := base64.RawURLEncoding.EncodeToString(mustMarshal(t, payload))
	signingInput := hBytes + "." + pBytes
	h := sha256.Sum256([]byte(signingInput))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, tk.priv, crypto.SHA256, h[:])
	token := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	_, err := validator.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected 'expired' in error, got: %v", err)
	}
}

// TestSMARTValidate_WrongIssuer ensures a token with an unexpected issuer is
// rejected.
func TestSMARTValidate_WrongIssuer(t *testing.T) {
	tk := newTestKey(t, "key-003")
	jwksSrv := mockJWKSServer(t, tk)

	const actualIssuer = "https://other.example.com"
	validator := NewSMARTValidator("https://smart.example.com", jwksSrv.URL)

	token := tk.sign(t, actualIssuer, nil)

	_, err := validator.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for issuer mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "issuer") {
		t.Errorf("expected 'issuer' in error, got: %v", err)
	}
}

// TestSMARTValidate_BadSignature ensures a JWT signed with a different key is
// rejected even when the kid points to a valid key in the JWKS.
func TestSMARTValidate_BadSignature(t *testing.T) {
	tk := newTestKey(t, "key-004")
	jwksSrv := mockJWKSServer(t, tk)

	const issuer = "https://smart.example.com"
	validator := NewSMARTValidator(issuer, jwksSrv.URL)

	token := wrongKeySign(t, issuer, tk)

	_, err := validator.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for bad signature, got nil")
	}
}

// TestSMARTValidate_UnknownKID ensures a token with a kid not in the JWKS is
// rejected.
func TestSMARTValidate_UnknownKID(t *testing.T) {
	tk := newTestKey(t, "key-005")
	jwksSrv := mockJWKSServer(t, tk)

	const issuer = "https://smart.example.com"
	validator := NewSMARTValidator(issuer, jwksSrv.URL)

	// Sign with a different kid that is not in the JWKS.
	unknownKIDKey := &testKey{priv: tk.priv, kid: "unknown-kid-999"}
	token := unknownKIDKey.sign(t, issuer, nil)

	_, err := validator.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for unknown kid, got nil")
	}
}

// TestSMARTValidate_JWKSCaching verifies that the JWKS endpoint is only called
// once across multiple Validate calls within the cache window.
func TestSMARTValidate_JWKSCaching(t *testing.T) {
	tk := newTestKey(t, "key-cache")
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(tk.jwksDoc)
	}))
	t.Cleanup(srv.Close)

	const issuer = "https://smart.example.com"
	validator := NewSMARTValidator(issuer, srv.URL)

	for i := 0; i < 5; i++ {
		token := tk.sign(t, issuer, nil)
		if _, err := validator.Validate(context.Background(), token); err != nil {
			t.Fatalf("Validate call %d: %v", i, err)
		}
	}
	if callCount != 1 {
		t.Errorf("JWKS endpoint called %d times, want 1 (cached)", callCount)
	}
}

// ─── HasScope tests ──────────────────────────────────────────────────────────

// TestSMARTHasScope covers the basic scope membership check.
func TestSMARTHasScope(t *testing.T) {
	validator := NewSMARTValidator("iss", "http://unused")
	claims := &SMARTClaims{Scope: "openid patient/Observation.read launch/patient"}

	for _, tc := range []struct {
		scope string
		want  bool
	}{
		{"openid", true},
		{"patient/Observation.read", true},
		{"launch/patient", true},
		{"patient/Patient.read", false},
		{"user/*.read", false},
		{"", false},
	} {
		got := validator.HasScope(claims, tc.scope)
		if got != tc.want {
			t.Errorf("HasScope(%q) = %v, want %v", tc.scope, got, tc.want)
		}
	}
}

// TestSMARTHasScope_NilClaims checks that HasScope does not panic on nil.
func TestSMARTHasScope_NilClaims(t *testing.T) {
	validator := NewSMARTValidator("iss", "http://unused")
	if validator.HasScope(nil, "openid") {
		t.Error("HasScope(nil, ...) should return false")
	}
}

// ─── ParseScopes tests ───────────────────────────────────────────────────────

// TestParseScopes checks correct classification of all scope categories.
func TestParseScopes(t *testing.T) {
	scopeStr := "openid fhirUser profile " +
		"patient/Observation.read patient/*.read " +
		"user/Patient.read user/*.write " +
		"system/Patient.read " +
		"launch launch/patient launch/encounter " +
		"offline_access"

	s := ParseScopes(scopeStr)

	checkContains := func(slice []string, want string, label string) {
		t.Helper()
		for _, v := range slice {
			if v == want {
				return
			}
		}
		t.Errorf("%s: %q not found in %v", label, want, slice)
	}

	checkContains(s.OpenID, "openid", "OpenID")
	checkContains(s.OpenID, "fhirUser", "OpenID")
	checkContains(s.OpenID, "profile", "OpenID")
	checkContains(s.Patient, "patient/Observation.read", "Patient")
	checkContains(s.Patient, "patient/*.read", "Patient")
	checkContains(s.User, "user/Patient.read", "User")
	checkContains(s.User, "user/*.write", "User")
	checkContains(s.System, "system/Patient.read", "System")
	checkContains(s.Launch, "launch", "Launch")
	checkContains(s.Launch, "launch/patient", "Launch")
	checkContains(s.Launch, "launch/encounter", "Launch")

	// offline_access is not classified into any category.
	for _, cat := range [][]string{s.Patient, s.User, s.System, s.Launch, s.OpenID} {
		for _, v := range cat {
			if v == "offline_access" {
				t.Error("offline_access should not appear in any category")
			}
		}
	}
}

// TestParseScopes_Empty verifies empty input returns empty struct.
func TestParseScopes_Empty(t *testing.T) {
	s := ParseScopes("")
	if len(s.Patient)+len(s.User)+len(s.System)+len(s.Launch)+len(s.OpenID) != 0 {
		t.Error("ParseScopes(\"\") should return all-empty SMARTScopes")
	}
}

// ─── BuildSMARTScopes tests ──────────────────────────────────────────────────

// TestBuildSMARTScopes checks the concatenation helper.
func TestBuildSMARTScopes(t *testing.T) {
	for _, tc := range []struct {
		resource, action, context, want string
	}{
		{"Observation", "read", "patient", "patient/Observation.read"},
		{"Patient", "write", "user", "user/Patient.write"},
		{"*", "*", "system", "system/*.*"},
		{"*", "read", "patient", "patient/*.read"},
	} {
		got := BuildSMARTScopes(tc.resource, tc.action, tc.context)
		if got != tc.want {
			t.Errorf("BuildSMARTScopes(%q,%q,%q) = %q, want %q",
				tc.resource, tc.action, tc.context, got, tc.want)
		}
	}
}

// ─── JWKS endpoint failure tests ─────────────────────────────────────────────

// TestSMARTValidate_JWKSUnavailable checks that a non-200 JWKS response yields
// an error.
func TestSMARTValidate_JWKSUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	validator := NewSMARTValidator("https://smart.example.com", srv.URL)

	// Build a syntactically valid token so the error comes from the JWKS fetch
	// path, not from header/payload parsing.
	tk := newTestKey(t, "k")
	token := tk.sign(t, "https://smart.example.com", nil)

	_, err := validator.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error when JWKS endpoint is unavailable")
	}
}

// TestSMARTValidate_MalformedToken checks that a token with fewer than 3 parts
// is rejected immediately.
func TestSMARTValidate_MalformedToken(t *testing.T) {
	validator := NewSMARTValidator("iss", "http://unused")
	_, err := validator.Validate(context.Background(), "notavalidjwt")
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}

// ─── PKIX round-trip (internal helper) ───────────────────────────────────────

// TestJWKRSAToDER verifies that a RSA public key survives a JWK → DER →
// *rsa.PublicKey round-trip and produces a key that passes a signature
// verification.
func TestJWKRSAToDER(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Build JWK from the public key.
	pub := &priv.PublicKey
	eBig := new(big.Int).SetInt64(int64(pub.E))
	k := jwk{
		Kid: "test",
		Kty: "RSA",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(eBig.Bytes()),
	}

	der, err := jwkRSAToDER(k)
	if err != nil {
		t.Fatalf("jwkRSAToDER: %v", err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatalf("ParsePKIXPublicKey: %v", err)
	}
	rsaPub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		t.Fatal("not an *rsa.PublicKey")
	}

	// Round-trip: sign with priv, verify with reconstructed pub.
	msg := []byte("hello")
	h := sha256.Sum256(msg)
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, h[:], sig); err != nil {
		t.Errorf("verify with reconstructed key failed: %v", err)
	}
}
