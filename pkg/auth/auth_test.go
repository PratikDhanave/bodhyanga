// Tests for pkg/auth — adapted in spirit from upstream auth_test.go but
// rewritten for the Bodhyanga-specific Claims + role catalogue + Rego policies.
//
// Coverage:
//   - Token roundtrip (Generate + Parse against the same keystore).
//   - Role-based gate (clinician allowed, nurse denied on explain).
//   - Tenant scope mismatch -> 403 (Evaluate returns ErrForbidden).
//   - Expired token -> ErrInvalidToken.

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// newTestAuthenticator generates a fresh RSA keypair, loads it into a
// KeyStore under the supplied kid, and returns a configured Authenticator
// + the kid + the keystore. Test helper used by every case below.
func newTestAuthenticator(t *testing.T) (*Authenticator, string, *KeyStore) {
	t.Helper()
	priv, err := GenerateRSAKey(2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	ks := NewKeyStore()
	const kid = "test-kid-001"
	if err := ks.LoadRSAPEM(kid, priv); err != nil {
		t.Fatalf("load pem: %v", err)
	}
	a := New(Config{KeyLookup: ks, Issuer: "bodhyanga-test"})
	return a, kid, ks
}

func TestAuthenticator_TokenRoundtrip(t *testing.T) {
	a, kid, _ := newTestAuthenticator(t)

	want := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    "bodhyanga-test",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: "tenant-a",
		Roles:    []string{RoleClinician},
	}
	signed, err := a.GenerateToken(kid, want)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	got, err := a.Parse(context.Background(), "Bearer "+signed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Subject != want.Subject || got.TenantID != want.TenantID || len(got.Roles) != 1 || got.Roles[0] != RoleClinician {
		t.Fatalf("claims mismatch: got=%+v want subject=%s tenant=%s role=%s", got, want.Subject, want.TenantID, RoleClinician)
	}
}

func TestAuthenticator_ExpiredToken(t *testing.T) {
	a, kid, _ := newTestAuthenticator(t)
	expired := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    "bodhyanga-test",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // an hour ago
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
		TenantID: "tenant-a",
		Roles:    []string{RoleClinician},
	}
	signed, err := a.GenerateToken(kid, expired)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := a.Parse(context.Background(), "Bearer "+signed); !errors.Is(err, ErrInvalidToken) {
		// jwt/v5 wraps the expiry error; we only require that the surface
		// returned to callers is ErrInvalidToken (so middleware -> 401).
		t.Fatalf("expected ErrInvalidToken on expired token, got %v", err)
	}
}

func TestAuthenticator_MissingBearer(t *testing.T) {
	a, _, _ := newTestAuthenticator(t)
	if _, err := a.Parse(context.Background(), ""); !errors.Is(err, ErrMissingBearer) {
		t.Fatalf("expected ErrMissingBearer, got %v", err)
	}
	if _, err := a.Parse(context.Background(), "Basic abc"); !errors.Is(err, ErrMissingBearer) {
		t.Fatalf("expected ErrMissingBearer for Basic header, got %v", err)
	}
}

func TestAuthorize_ExplainRoleGate(t *testing.T) {
	a, _, _ := newTestAuthenticator(t)
	ctx := context.Background()

	// Same-tenant clinician -> allowed.
	if err := a.Evaluate(ctx, ModuleExplain, QueryExplainAllow, map[string]any{
		"Roles":            []string{RoleClinician},
		"TenantID":         "tenant-a",
		"ResourceTenantID": "tenant-a",
	}); err != nil {
		t.Errorf("clinician same-tenant: want allow, got %v", err)
	}

	// Same-tenant compliance_officer -> allowed.
	if err := a.Evaluate(ctx, ModuleExplain, QueryExplainAllow, map[string]any{
		"Roles":            []string{RoleComplianceOfficer},
		"TenantID":         "tenant-a",
		"ResourceTenantID": "tenant-a",
	}); err != nil {
		t.Errorf("compliance_officer same-tenant: want allow, got %v", err)
	}

	// Cross-tenant auditor -> allowed (auditor reads across tenants).
	if err := a.Evaluate(ctx, ModuleExplain, QueryExplainAllow, map[string]any{
		"Roles":            []string{RoleAuditor},
		"TenantID":         "tenant-z",
		"ResourceTenantID": "tenant-a",
	}); err != nil {
		t.Errorf("auditor cross-tenant: want allow, got %v", err)
	}

	// Nurse -> denied (HITL invariant: nurse can list reviews but not read
	// explanations; explanation surface is clinician/compliance/auditor).
	if err := a.Evaluate(ctx, ModuleExplain, QueryExplainAllow, map[string]any{
		"Roles":            []string{RoleNurse},
		"TenantID":         "tenant-a",
		"ResourceTenantID": "tenant-a",
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("nurse: want ErrForbidden, got %v", err)
	}

	// Clinician CROSS-tenant -> denied.
	if err := a.Evaluate(ctx, ModuleExplain, QueryExplainAllow, map[string]any{
		"Roles":            []string{RoleClinician},
		"TenantID":         "tenant-other",
		"ResourceTenantID": "tenant-a",
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("clinician cross-tenant: want ErrForbidden, got %v", err)
	}
}

func TestAuthorize_ReviewsGate(t *testing.T) {
	a, _, _ := newTestAuthenticator(t)
	ctx := context.Background()

	// Nurse may GET (list) reviews in their tenant.
	if err := a.Evaluate(ctx, ModuleReviews, QueryReviewsAllow, map[string]any{
		"Roles":            []string{RoleNurse},
		"Method":           "GET",
		"Path":             "/api/reviews",
		"TenantID":         "tenant-a",
		"ResourceTenantID": "tenant-a",
	}); err != nil {
		t.Errorf("nurse GET reviews: want allow, got %v", err)
	}

	// Nurse may NOT decide.
	if err := a.Evaluate(ctx, ModuleReviews, QueryReviewsAllow, map[string]any{
		"Roles":            []string{RoleNurse},
		"Method":           "POST",
		"Path":             "/api/reviews/abc/decide",
		"TenantID":         "tenant-a",
		"ResourceTenantID": "tenant-a",
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("nurse POST decide: want ErrForbidden, got %v", err)
	}

	// Clinician may decide.
	if err := a.Evaluate(ctx, ModuleReviews, QueryReviewsAllow, map[string]any{
		"Roles":            []string{RoleClinician},
		"Method":           "POST",
		"Path":             "/api/reviews/abc/decide",
		"TenantID":         "tenant-a",
		"ResourceTenantID": "tenant-a",
	}); err != nil {
		t.Errorf("clinician POST decide: want allow, got %v", err)
	}
}

func TestAuthorize_AuditGate(t *testing.T) {
	a, _, _ := newTestAuthenticator(t)
	ctx := context.Background()

	if err := a.Evaluate(ctx, ModuleAudit, QueryAuditAllow, map[string]any{
		"Roles": []string{RoleAuditor},
	}); err != nil {
		t.Errorf("auditor: want allow, got %v", err)
	}
	if err := a.Evaluate(ctx, ModuleAudit, QueryAuditAllow, map[string]any{
		"Roles": []string{RoleClinician},
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("clinician on audit: want ErrForbidden, got %v", err)
	}
}
