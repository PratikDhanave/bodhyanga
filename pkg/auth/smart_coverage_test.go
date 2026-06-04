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
	"testing"
	"time"
)

// helper to build a signed RS256 JWT for SMART tests.
func buildSMARTToken(t *testing.T, key *rsa.PrivateKey, kid string, claims SMARTClaims) string {
	t.Helper()

	header := map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := headerB64 + "." + claimsB64
	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		t.Fatal(err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return headerB64 + "." + claimsB64 + "." + sigB64
}

// helper to build a JWKS server returning the given public key.
func jwksServer(t *testing.T, key *rsa.PublicKey, kid string) *httptest.Server {
	t.Helper()
	nB64 := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	eBuf := big.NewInt(int64(key.E)).Bytes()
	eB64 := base64.RawURLEncoding.EncodeToString(eBuf)

	jwksResp := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"kid": kid,
				"alg": "RS256",
				"use": "sig",
				"n":   nB64,
				"e":   eB64,
			},
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jwksResp)
	}))
}

func TestSMARTValidator_ValidToken(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "test-kid"
	srv := jwksServer(t, &key.PublicKey, kid)
	defer srv.Close()

	v := NewSMARTValidator("https://auth.example.com", srv.URL)

	claims := SMARTClaims{
		Sub:      "user-1",
		Iss:      "https://auth.example.com",
		Exp:      time.Now().Add(time.Hour).Unix(),
		Iat:      time.Now().Unix(),
		Scope:    "patient/Observation.read openid",
		ClientID: "app-1",
	}

	token := buildSMARTToken(t, key, kid, claims)
	parsed, err := v.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
	if parsed.Sub != "user-1" {
		t.Fatalf("expected user-1, got %q", parsed.Sub)
	}
}

func TestSMARTValidator_MalformedJWT(t *testing.T) {
	v := NewSMARTValidator("iss", "http://localhost")
	_, err := v.Validate(context.Background(), "not.a.jwt.at.all")
	if err == nil {
		t.Fatal("expected error for malformed JWT")
	}
}

func TestSMARTValidator_OnlyTwoParts(t *testing.T) {
	v := NewSMARTValidator("iss", "http://localhost")
	_, err := v.Validate(context.Background(), "header.payload")
	if err == nil {
		t.Fatal("expected error for 2-part JWT")
	}
}

func TestSMARTValidator_BadHeaderBase64(t *testing.T) {
	v := NewSMARTValidator("iss", "http://localhost")
	_, err := v.Validate(context.Background(), "!!!.payload.sig")
	if err == nil {
		t.Fatal("expected error for bad header base64")
	}
}

func TestSMARTValidator_UnsupportedAlg(t *testing.T) {
	v := NewSMARTValidator("iss", "http://localhost")
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","kid":"k1"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{}`))
	_, err := v.Validate(context.Background(), header+"."+payload+".sig")
	if err == nil {
		t.Fatal("expected error for HS256 alg")
	}
}

func TestSMARTValidator_BadPayloadBase64(t *testing.T) {
	v := NewSMARTValidator("iss", "http://localhost")
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k1"}`))
	_, err := v.Validate(context.Background(), header+".!!!.sig")
	if err == nil {
		t.Fatal("expected error for bad payload base64")
	}
}

func TestSMARTValidator_BadPayloadJSON(t *testing.T) {
	v := NewSMARTValidator("iss", "http://localhost")
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k1"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`not json`))
	_, err := v.Validate(context.Background(), header+"."+payload+".sig")
	if err == nil {
		t.Fatal("expected error for bad payload JSON")
	}
}

func TestSMARTValidator_UnknownKID(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := jwksServer(t, &key.PublicKey, "correct-kid")
	defer srv.Close()

	v := NewSMARTValidator("iss", srv.URL)

	claims := SMARTClaims{
		Sub: "user-1",
		Iss: "iss",
		Exp: time.Now().Add(time.Hour).Unix(),
	}
	token := buildSMARTToken(t, key, "wrong-kid", claims)
	_, err := v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for unknown kid")
	}
}

func TestSMARTValidator_ExpiredToken(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "kid-1"
	srv := jwksServer(t, &key.PublicKey, kid)
	defer srv.Close()

	v := NewSMARTValidator("iss", srv.URL)

	claims := SMARTClaims{
		Sub: "user-1",
		Iss: "iss",
		Exp: time.Now().Add(-time.Hour).Unix(),
	}
	token := buildSMARTToken(t, key, kid, claims)
	_, err := v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestSMARTValidator_NoExpClaim(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "kid-1"
	srv := jwksServer(t, &key.PublicKey, kid)
	defer srv.Close()

	v := NewSMARTValidator("iss", srv.URL)

	claims := SMARTClaims{
		Sub: "user-1",
		Iss: "iss",
		Exp: 0,
	}
	token := buildSMARTToken(t, key, kid, claims)
	_, err := v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for missing exp")
	}
}

func TestSMARTValidator_IssuerMismatch(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "kid-1"
	srv := jwksServer(t, &key.PublicKey, kid)
	defer srv.Close()

	v := NewSMARTValidator("expected-iss", srv.URL)

	claims := SMARTClaims{
		Sub: "user-1",
		Iss: "wrong-iss",
		Exp: time.Now().Add(time.Hour).Unix(),
	}
	token := buildSMARTToken(t, key, kid, claims)
	_, err := v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for issuer mismatch")
	}
}

func TestSMARTValidator_BadSignature(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "kid-1"
	srv := jwksServer(t, &key.PublicKey, kid) // Server has key's public key
	defer srv.Close()

	v := NewSMARTValidator("iss", srv.URL)

	claims := SMARTClaims{
		Sub: "user-1",
		Iss: "iss",
		Exp: time.Now().Add(time.Hour).Unix(),
	}
	// Sign with a different key.
	token := buildSMARTToken(t, otherKey, kid, claims)
	_, err := v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for bad signature")
	}
}

func TestSMARTValidator_BadSignatureBase64(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "kid-1"
	srv := jwksServer(t, &key.PublicKey, kid)
	defer srv.Close()

	v := NewSMARTValidator("iss", srv.URL)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"kid-1"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u","iss":"iss","exp":99999999999}`))
	_, err := v.Validate(context.Background(), header+"."+payload+".!!!")
	if err == nil {
		t.Fatal("expected error for bad signature base64")
	}
}

func TestSMARTValidator_FetchKeysError(t *testing.T) {
	// Server that returns non-200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	v := NewSMARTValidator("iss", srv.URL)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k1"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u","iss":"iss","exp":99999999999}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("fakesig"))
	_, err := v.Validate(context.Background(), header+"."+payload+"."+sig)
	if err == nil {
		t.Fatal("expected error for failed JWKS fetch")
	}
}

func TestSMARTValidator_FetchKeysBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	v := NewSMARTValidator("iss", srv.URL)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k1"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u","iss":"iss","exp":99999999999}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("fakesig"))
	_, err := v.Validate(context.Background(), header+"."+payload+"."+sig)
	if err == nil {
		t.Fatal("expected error for bad JWKS JSON")
	}
}

func TestSMARTValidator_FetchKeysSkipsNonRSA(t *testing.T) {
	// JWKS with a non-RSA key.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{"kty": "EC", "kid": "ec-key"},
			},
		})
	}))
	defer srv.Close()

	v := NewSMARTValidator("iss", srv.URL)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"ec-key"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u","iss":"iss","exp":99999999999}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("fakesig"))
	_, err := v.Validate(context.Background(), header+"."+payload+"."+sig)
	if err == nil {
		t.Fatal("expected error — EC key should be skipped, kid not found")
	}
}

func TestSMARTValidator_CacheRefresh(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "kid-1"
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		nB64 := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
		eBuf := big.NewInt(int64(key.PublicKey.E)).Bytes()
		eB64 := base64.RawURLEncoding.EncodeToString(eBuf)
		json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{"kty": "RSA", "kid": kid, "n": nB64, "e": eB64},
			},
		})
	}))
	defer srv.Close()

	v := NewSMARTValidator("iss", srv.URL)

	claims := SMARTClaims{Sub: "u", Iss: "iss", Exp: time.Now().Add(time.Hour).Unix()}
	token := buildSMARTToken(t, key, kid, claims)

	// First call fetches keys.
	_, err := v.Validate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}

	// Second call should use cache.
	_, err = v.Validate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}

	if callCount != 1 {
		t.Fatalf("expected 1 JWKS fetch (cache hit), got %d", callCount)
	}
}

func TestSMARTValidator_HasScope(t *testing.T) {
	v := NewSMARTValidator("iss", "http://localhost")

	if v.HasScope(nil, "any") {
		t.Fatal("expected false for nil claims")
	}

	claims := &SMARTClaims{Scope: "patient/Observation.read openid fhirUser"}
	if !v.HasScope(claims, "openid") {
		t.Fatal("expected openid to be present")
	}
	if v.HasScope(claims, "system/Patient.read") {
		t.Fatal("expected system/Patient.read to be absent")
	}
}

func TestJWKRSAToDER_BadN(t *testing.T) {
	_, err := jwkRSAToDER(jwk{N: "!!!", E: "AQAB"})
	if err == nil {
		t.Fatal("expected error for bad n")
	}
}

func TestJWKRSAToDER_BadE(t *testing.T) {
	_, err := jwkRSAToDER(jwk{N: "AQAB", E: "!!!"})
	if err == nil {
		t.Fatal("expected error for bad e")
	}
}

func TestJWKRSAToDER_OversizedE(t *testing.T) {
	_, err := jwkRSAToDER(jwk{N: "AQAB", E: "AQABAQAB"}) // > 4 bytes
	if err == nil {
		t.Fatal("expected error for oversized e")
	}
}

func TestJWKRSAToDER_ValidKey(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	nB64 := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	eBuf := big.NewInt(int64(key.PublicKey.E)).Bytes()
	eB64 := base64.RawURLEncoding.EncodeToString(eBuf)

	der, err := jwkRSAToDER(jwk{N: nB64, E: eB64, Kty: "RSA"})
	if err != nil {
		t.Fatal(err)
	}

	// Should be valid PKIX public key.
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pub.(*rsa.PublicKey); !ok {
		t.Fatal("expected RSA public key")
	}
}
