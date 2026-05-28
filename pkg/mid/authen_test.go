// Tests for pkg/mid/authen.go + authorize.go — end-to-end HTTP behaviour
// for the JWT + Rego stack. Covers the test scenarios spelled out in
// docs/auth.md:
//
//   - valid clinician  -> 200
//   - valid auditor cross-tenant on explain -> 200
//   - nurse -> 403 on explain
//   - missing token (jwt mode) -> 401
//   - expired token -> 401
//   - wrong tenant -> 403
//
// Plus the dev-mode escape hatch: Authenticator == nil bypasses both
// middlewares so cmd/care continues to work with BODH_AUTH=disabled.

package mid

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/PratikDhanave/bodh/pkg/auth"
)

// newAuth builds a one-kid Authenticator + a helper that mints a token
// for the given claims. Test infrastructure used by every case below.
func newAuth(t *testing.T) (*auth.Authenticator, func(claims auth.Claims) string) {
	t.Helper()
	priv, err := auth.GenerateRSAKey(2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	ks := auth.NewKeyStore()
	const kid = "test"
	if err := ks.LoadRSAPEM(kid, priv); err != nil {
		t.Fatalf("load pem: %v", err)
	}
	a := auth.New(auth.Config{KeyLookup: ks, Issuer: "bodh-test"})
	mint := func(c auth.Claims) string {
		if c.Issuer == "" {
			c.Issuer = "bodh-test"
		}
		if c.ExpiresAt == nil {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
		}
		s, err := a.GenerateToken(kid, c)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		return s
	}
	return a, mint
}

// okHandler is the inner handler under test — writes 200 + a marker so
// tests can assert the chain actually passed through (vs. being short-
// circuited at 401/403).
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
})

func TestAuthenticate_DisabledBypass(t *testing.T) {
	// Authenticator == nil -> middleware no-ops. BODH_AUTH=disabled
	// preserves current dev UX.
	h := Authenticate(nil)(okHandler)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/explain/c1", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("disabled mode should pass through, got %d", rr.Code)
	}
}

func TestAuthenticate_MissingToken(t *testing.T) {
	a, _ := newAuth(t)
	h := Authenticate(a)(okHandler)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/explain/c1", nil) // no Authorization header
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: want 401, got %d", rr.Code)
	}
}

func TestAuthenticate_ExpiredToken(t *testing.T) {
	a, mint := newAuth(t)
	token := mint(auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
		TenantID: "t1",
		Roles:    []string{auth.RoleClinician},
	})
	h := Authenticate(a)(okHandler)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/explain/c1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: want 401, got %d", rr.Code)
	}
}

func TestAuthorizeExplain_ClinicianSameTenant(t *testing.T) {
	a, mint := newAuth(t)
	token := mint(auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u"},
		TenantID:         "tenant-a",
		Roles:            []string{auth.RoleClinician},
	})
	chain := Authenticate(a)(AuthorizeExplain(a, "tenant-a")(okHandler))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/explain/c1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	chain.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("clinician same-tenant: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAuthorizeExplain_AuditorCrossTenant(t *testing.T) {
	a, mint := newAuth(t)
	token := mint(auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u"},
		TenantID:         "tenant-other", // auditor's home tenant
		Roles:            []string{auth.RoleAuditor},
	})
	chain := Authenticate(a)(AuthorizeExplain(a, "tenant-a")(okHandler))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/explain/c1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	chain.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("auditor cross-tenant: want 200, got %d", rr.Code)
	}
}

func TestAuthorizeExplain_NurseForbidden(t *testing.T) {
	a, mint := newAuth(t)
	token := mint(auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u"},
		TenantID:         "tenant-a",
		Roles:            []string{auth.RoleNurse},
	})
	chain := Authenticate(a)(AuthorizeExplain(a, "tenant-a")(okHandler))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/explain/c1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	chain.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("nurse on explain: want 403, got %d", rr.Code)
	}
}

func TestAuthorizeExplain_WrongTenant(t *testing.T) {
	a, mint := newAuth(t)
	token := mint(auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u"},
		TenantID:         "tenant-other",
		Roles:            []string{auth.RoleClinician},
	})
	chain := Authenticate(a)(AuthorizeExplain(a, "tenant-a")(okHandler))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/explain/c1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	chain.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("clinician wrong tenant: want 403, got %d", rr.Code)
	}
}

func TestAuthorizeReviews_NurseListAllowedDecideForbidden(t *testing.T) {
	a, mint := newAuth(t)
	token := mint(auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u"},
		TenantID:         "tenant-a",
		Roles:            []string{auth.RoleNurse},
	})
	chain := Authenticate(a)(AuthorizeReviews(a, "tenant-a")(okHandler))

	// GET /api/reviews -> nurse allowed.
	{
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/reviews", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		chain.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("nurse GET reviews: want 200, got %d", rr.Code)
		}
	}
	// POST /api/reviews/x/decide -> nurse forbidden.
	{
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/reviews/x/decide", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		chain.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("nurse POST decide: want 403, got %d", rr.Code)
		}
	}
}

func TestAuthorizeAudit_AuditorOnly(t *testing.T) {
	a, mint := newAuth(t)
	tokenAuditor := mint(auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u"},
		TenantID:         "tenant-a",
		Roles:            []string{auth.RoleAuditor},
	})
	tokenClinician := mint(auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u"},
		TenantID:         "tenant-a",
		Roles:            []string{auth.RoleClinician},
	})
	chain := Authenticate(a)(AuthorizeAudit(a)(okHandler))

	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{"auditor", tokenAuditor, http.StatusOK},
		{"clinician", tokenClinician, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			chain.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("%s: want %d, got %d", tc.name, tc.want, rr.Code)
			}
		})
	}
}
