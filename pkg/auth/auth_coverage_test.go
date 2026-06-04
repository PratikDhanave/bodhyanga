package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"testing/fstest"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// Authenticator tests
// ---------------------------------------------------------------------------

func TestAuthenticator_Issuer(t *testing.T) {
	ks := NewKeyStore()
	a := New(Config{KeyLookup: ks, Issuer: "bodh-test"})
	if a.Issuer() != "bodh-test" {
		t.Fatalf("expected bodh-test, got %q", a.Issuer())
	}
}

func TestAuthenticator_ParseMissingBearer(t *testing.T) {
	ks := NewKeyStore()
	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	_, err := a.Parse(context.Background(), "not-a-bearer-token")
	if err != ErrMissingBearer {
		t.Fatalf("expected ErrMissingBearer, got %v", err)
	}
}

func TestAuthenticator_ParseMalformedToken(t *testing.T) {
	ks := NewKeyStore()
	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	_, err := a.Parse(context.Background(), "Bearer not.a.jwt")
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestAuthenticator_ParseNoKID(t *testing.T) {
	ks := NewKeyStore()
	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	// Create a token without a kid header.
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "user-1",
		"iss": "bodh",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	// Don't set kid header.
	signed, _ := tok.SignedString(key)

	_, err := a.Parse(context.Background(), "Bearer "+signed)
	if err != ErrKIDMissing {
		t.Fatalf("expected ErrKIDMissing, got %v", err)
	}
}

func TestAuthenticator_ParseMalformedKID(t *testing.T) {
	ks := NewKeyStore()
	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "user-1",
		"iss": "bodh",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = 12345 // not a string
	signed, _ := tok.SignedString(key)

	_, err := a.Parse(context.Background(), "Bearer "+signed)
	if err != ErrKIDMalformed {
		t.Fatalf("expected ErrKIDMalformed, got %v", err)
	}
}

func TestAuthenticator_ParseUnknownKID(t *testing.T) {
	ks := NewKeyStore()
	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "user-1",
		"iss": "bodh",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "unknown-kid"
	signed, _ := tok.SignedString(key)

	_, err := a.Parse(context.Background(), "Bearer "+signed)
	if err == nil {
		t.Fatal("expected error for unknown kid")
	}
}

func TestAuthenticator_GenerateAndParseRoundTrip(t *testing.T) {
	ks := NewKeyStore()
	privPEM, err := GenerateRSAKey(2048)
	if err != nil {
		t.Fatal(err)
	}
	if err := ks.LoadRSAPEM("kid-1", privPEM); err != nil {
		t.Fatal(err)
	}

	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "bodh",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: "tenant-1",
		Roles:    []string{RoleClinician},
	}

	token, err := a.GenerateToken("kid-1", claims)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := a.Parse(context.Background(), "Bearer "+token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Subject != "user-1" {
		t.Fatalf("expected user-1, got %q", parsed.Subject)
	}
	if parsed.TenantID != "tenant-1" {
		t.Fatalf("expected tenant-1, got %q", parsed.TenantID)
	}
}

func TestAuthenticator_ParseExpiredToken(t *testing.T) {
	ks := NewKeyStore()
	privPEM, _ := GenerateRSAKey(2048)
	ks.LoadRSAPEM("kid-1", privPEM)

	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "bodh",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}

	token, _ := a.GenerateToken("kid-1", claims)
	_, err := a.Parse(context.Background(), "Bearer "+token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestAuthenticator_GenerateTokenBadKID(t *testing.T) {
	ks := NewKeyStore()
	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	_, err := a.GenerateToken("nonexistent", Claims{})
	if err == nil {
		t.Fatal("expected error for unknown kid")
	}
}

func TestAuthenticator_EvaluateDenied(t *testing.T) {
	ks := NewKeyStore()
	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	// Use the explain module; a request without clinician role should be denied.
	err := a.Evaluate(context.Background(), ModuleExplain, QueryExplainAllow, map[string]any{
		"Roles": []string{"viewer"},
	})
	if err != ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestAuthenticator_EvaluateAllowed(t *testing.T) {
	ks := NewKeyStore()
	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	// The explain policy requires TenantID == ResourceTenantID for clinician.
	err := a.Evaluate(context.Background(), ModuleExplain, QueryExplainAllow, map[string]any{
		"Roles":            []string{RoleClinician},
		"TenantID":         "t1",
		"ResourceTenantID": "t1",
	})
	if err != nil {
		t.Fatalf("expected allowed, got %v", err)
	}
}

func TestAuthenticator_EvaluateAuditorCrossTenant(t *testing.T) {
	ks := NewKeyStore()
	a := New(Config{KeyLookup: ks, Issuer: "bodh"})

	err := a.Evaluate(context.Background(), ModuleExplain, QueryExplainAllow, map[string]any{
		"Roles":            []string{RoleAuditor},
		"TenantID":         "t1",
		"ResourceTenantID": "t2",
	})
	if err != nil {
		t.Fatalf("expected auditor allowed cross-tenant, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// KeyStore tests
// ---------------------------------------------------------------------------

func TestKeyStore_PrivateKeyNotFound(t *testing.T) {
	ks := NewKeyStore()
	_, err := ks.PrivateKey("nonexistent")
	if err != ErrKeyNotFound {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestKeyStore_PublicKeyNotFound(t *testing.T) {
	ks := NewKeyStore()
	_, err := ks.PublicKey("nonexistent")
	if err != ErrKeyNotFound {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestKeyStore_LoadRSAPEM_InvalidPEM(t *testing.T) {
	ks := NewKeyStore()
	err := ks.LoadRSAPEM("bad", "not a pem")
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestKeyStore_LoadByFileSystem(t *testing.T) {
	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})

	fs := fstest.MapFS{
		"test-key.pem": &fstest.MapFile{Data: privPEM},
		"readme.txt":   &fstest.MapFile{Data: []byte("not a pem")},
	}

	ks := NewKeyStore()
	n, err := ks.LoadByFileSystem(fs)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 key loaded, got %d", n)
	}

	_, err = ks.PrivateKey("test-key")
	if err != nil {
		t.Fatalf("expected key, got: %v", err)
	}
}

func TestKeyStore_LoadByFileSystem_BadPEM(t *testing.T) {
	fs := fstest.MapFS{
		"bad.pem": &fstest.MapFile{Data: []byte("not a pem file")},
	}

	ks := NewKeyStore()
	_, err := ks.LoadByFileSystem(fs)
	if err == nil {
		t.Fatal("expected error for bad PEM")
	}
}

func TestGenerateRSAKey_TooSmall(t *testing.T) {
	_, err := GenerateRSAKey(1024)
	if err == nil {
		t.Fatal("expected error for key < 2048 bits")
	}
}

func TestGenerateRSAKey_Success(t *testing.T) {
	privPEM, err := GenerateRSAKey(2048)
	if err != nil {
		t.Fatal(err)
	}
	if privPEM == "" {
		t.Fatal("expected non-empty PEM")
	}
	// Verify it's valid PEM.
	block, _ := pem.Decode([]byte(privPEM))
	if block == nil {
		t.Fatal("expected valid PEM block")
	}
}

func TestToPublicPEM_PKCS1(t *testing.T) {
	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})
	pubPEM, err := toPublicPEM(string(privPEM))
	if err != nil {
		t.Fatal(err)
	}
	if pubPEM == "" {
		t.Fatal("expected non-empty public PEM")
	}
}

func TestToPublicPEM_PKCS8(t *testing.T) {
	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	pkcs8Bytes, _ := x509.MarshalPKCS8PrivateKey(privKey)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	})
	pubPEM, err := toPublicPEM(string(privPEM))
	if err != nil {
		t.Fatal(err)
	}
	if pubPEM == "" {
		t.Fatal("expected non-empty public PEM")
	}
}

func TestToPublicPEM_NotPEM(t *testing.T) {
	_, err := toPublicPEM("not a pem")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestToPublicPEM_NotRSA(t *testing.T) {
	// Create a PEM block with garbage bytes.
	badPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: []byte("garbage"),
	})
	_, err := toPublicPEM(string(badPEM))
	if err == nil {
		t.Fatal("expected error for non-RSA key")
	}
}

// ---------------------------------------------------------------------------
// CredentialStore tests
// ---------------------------------------------------------------------------

func TestCredentialStore_VerifySuccess(t *testing.T) {
	s := NewCredentialStore()
	u, err := s.Verify("admin", "admin123")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != RoleAdmin {
		t.Fatalf("expected admin role, got %q", u.Role)
	}
}

func TestCredentialStore_VerifyWrongPassword(t *testing.T) {
	s := NewCredentialStore()
	_, err := s.Verify("admin", "wrong")
	if err != ErrInvalidPassword {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestCredentialStore_VerifyUnknownUser(t *testing.T) {
	s := NewCredentialStore()
	_, err := s.Verify("nobody", "pass")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestCredentialStore_IssueAndValidateToken(t *testing.T) {
	s := NewCredentialStore()
	u, _ := s.Verify("admin", "admin123")

	secret := "test-secret"
	token, err := s.IssueToken(u, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	validated, err := s.ValidateToken(token, secret)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Email != "admin" {
		t.Fatalf("expected admin email, got %q", validated.Email)
	}
}

func TestCredentialStore_ValidateTokenBadSecret(t *testing.T) {
	s := NewCredentialStore()
	u, _ := s.Verify("admin", "admin123")

	token, _ := s.IssueToken(u, "correct-secret", time.Hour)

	_, err := s.ValidateToken(token, "wrong-secret")
	if err != ErrInvalidCredentialToken {
		t.Fatalf("expected ErrInvalidCredentialToken, got %v", err)
	}
}

func TestCredentialStore_ValidateTokenExpired(t *testing.T) {
	s := NewCredentialStore()
	u, _ := s.Verify("admin", "admin123")

	// Issue with negative TTL so it's already expired.
	token, _ := s.IssueToken(u, "secret", -time.Hour)

	_, err := s.ValidateToken(token, "secret")
	if err != ErrInvalidCredentialToken {
		t.Fatalf("expected ErrInvalidCredentialToken, got %v", err)
	}
}

func TestCredentialStore_ValidateTokenMalformed(t *testing.T) {
	s := NewCredentialStore()
	_, err := s.ValidateToken("not.a.jwt", "secret")
	if err != ErrInvalidCredentialToken {
		t.Fatalf("expected ErrInvalidCredentialToken, got %v", err)
	}
}

func TestJWTSecret_Default(t *testing.T) {
	t.Setenv("BODH_JWT_SECRET", "")
	if JWTSecret() != DefaultJWTSecret {
		t.Fatalf("expected default secret, got %q", JWTSecret())
	}
}

func TestJWTSecret_Custom(t *testing.T) {
	t.Setenv("BODH_JWT_SECRET", "my-custom-secret")
	if JWTSecret() != "my-custom-secret" {
		t.Fatalf("expected my-custom-secret, got %q", JWTSecret())
	}
}
