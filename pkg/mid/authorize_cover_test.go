package mid

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/auth"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
	"github.com/PratikDhanave/bodhyanga/pkg/web"
)

// --- authorize: nil Authenticator bypass ---

func TestAuthorizeExplain_NilAuthenticator_Bypass(t *testing.T) {
	chain := AuthorizeExplain(nil, "tenant-a")(okHandler)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/explain/c1", nil)
	chain.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("nil authenticator: want 200, got %d", rr.Code)
	}
}

// --- authorize: no claims in context -> 401 ---

func TestAuthorize_NoClaims_Returns401(t *testing.T) {
	a, _ := newAuth(t)
	chain := AuthorizeExplain(a, "tenant-a")(okHandler)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/explain/c1", nil)
	chain.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no claims: want 401, got %d", rr.Code)
	}
}

// --- authorize: Evaluate returns non-ErrForbidden error -> 500 ---

func TestAuthorize_EvaluateError_Returns500(t *testing.T) {
	a, _ := newAuth(t)

	claims := auth.Claims{
		TenantID: "tenant-a",
		Roles:    []string{auth.RoleClinician},
	}
	ctx := WithClaims(context.Background(), claims)

	// Use authorize directly with a bogus module/query to trigger a
	// non-forbidden error from Evaluate.
	authMw := authorize(a, "nonexistent.module.that.does.not.exist", "data.nonexistent.allow",
		func(_ *http.Request, c auth.Claims) map[string]any {
			return map[string]any{"Roles": c.Roles}
		})

	handler := authMw(okHandler)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(ctx)
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("evaluate error: want 500, got %d", rr.Code)
	}
}

// --- authorize nil bypass for Reviews and Audit ---

func TestAuthorizeReviews_NilAuthenticator_Bypass(t *testing.T) {
	chain := AuthorizeReviews(nil, "tenant-a")(okHandler)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/reviews", nil)
	chain.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("nil authenticator: want 200, got %d", rr.Code)
	}
}

func TestAuthorizeAudit_NilAuthenticator_Bypass(t *testing.T) {
	chain := AuthorizeAudit(nil)(okHandler)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	chain.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("nil authenticator: want 200, got %d", rr.Code)
	}
}

// --- startNanos: no stamp returns current time ---

func TestStartNanos_NoStamp(t *testing.T) {
	ctx := context.Background()
	ns := startNanos(ctx)
	if ns == 0 {
		t.Fatal("startNanos with no stamp should return a non-zero value")
	}
}

// --- startNanos: with stamp returns the stamp ---

func TestStartNanos_WithStamp(t *testing.T) {
	ctx := web.StampStart(context.Background())
	stamp := web.GetStart(ctx)
	if stamp == 0 {
		t.Fatal("StampStart should produce a non-zero stamp")
	}
	ns := startNanos(ctx)
	if ns != stamp {
		t.Errorf("startNanos = %d, want stamp %d", ns, stamp)
	}
}

// --- Logger: handler returns generic error (not *errs.Error) ---

func TestLogger_GenericError(t *testing.T) {
	var buf bytes.Buffer
	raw := observability.NewLoggerTo(&buf)
	wrapped := Logger(raw)(func(ctx context.Context, r *http.Request) (web.Encoder, error) {
		return nil, context.DeadlineExceeded
	})
	_, _ = wrapped(context.Background(), httptest.NewRequest("GET", "/x", nil))
	if !strings.Contains(buf.String(), "code=internal") {
		t.Errorf("expected 'code=internal' in log output, got %q", buf.String())
	}
}

// --- Logger: query string logged ---

func TestLogger_QueryStringLogged(t *testing.T) {
	var buf bytes.Buffer
	raw := observability.NewLoggerTo(&buf)
	wrapped := Logger(raw)(func(ctx context.Context, r *http.Request) (web.Encoder, error) {
		return web.NewJSON(map[string]string{}), nil
	})
	req := httptest.NewRequest("GET", "/path?key=value", nil)
	_, _ = wrapped(context.Background(), req)
	if !strings.Contains(buf.String(), "key=value") {
		t.Errorf("expected query string in log, got %q", buf.String())
	}
}

// suppress "imported and not used" if fmt is only used implicitly
var _ = fmt.Sprintf
