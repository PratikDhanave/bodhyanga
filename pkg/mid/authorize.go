// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/mid/authorize.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga:
//   - stdlib net/http (not the upstream web.MidFunc).
//   - No homebus / productbus / userbus dependencies; Bodhyanga's resources
//     (explain / reviews / audit) gate on tenant + method + role only.
//   - Per-route Rego module selection: each Authorize* function pins the
//     module + query so the route table reads cleanly in cmd/care/main.go.

package mid

import (
	"errors"
	"net/http"

	"github.com/PratikDhanave/bodhyanga/pkg/auth"
)

// AuthorizeExplain wraps a handler with the bodhyanga.explain.allow Rego rule.
// On the request path the middleware reads Claims from context (must come
// AFTER Authenticate), builds the Rego input with the configured
// resource tenant, and either passes through or returns 403.
//
// resourceTenantID is the binary's BODHYANGA_TENANT_ID — single-tenant
// binaries pin it at boot; multi-tenant binaries would resolve per
// request from path / header.
func AuthorizeExplain(a *auth.Authenticator, resourceTenantID string) func(http.Handler) http.Handler {
	return authorize(a, auth.ModuleExplain, auth.QueryExplainAllow, func(r *http.Request, c auth.Claims) map[string]any {
		return map[string]any{
			"Roles":            c.Roles,
			"TenantID":         c.TenantID,
			"ResourceTenantID": resourceTenantID,
		}
	})
}

// AuthorizeReviews wraps a handler with the bodhyanga.reviews.allow Rego rule.
// The policy switches on input.Method so the same middleware can wrap
// GET /api/reviews and POST /api/reviews/{id}/decide.
func AuthorizeReviews(a *auth.Authenticator, resourceTenantID string) func(http.Handler) http.Handler {
	return authorize(a, auth.ModuleReviews, auth.QueryReviewsAllow, func(r *http.Request, c auth.Claims) map[string]any {
		return map[string]any{
			"Roles":            c.Roles,
			"Method":           r.Method,
			"Path":             r.URL.Path,
			"TenantID":         c.TenantID,
			"ResourceTenantID": resourceTenantID,
		}
	})
}

// AuthorizeAudit wraps a handler with the bodhyanga.audit.allow Rego rule
// (auditor role only; cross-tenant read by deployment policy).
func AuthorizeAudit(a *auth.Authenticator) func(http.Handler) http.Handler {
	return authorize(a, auth.ModuleAudit, auth.QueryAuditAllow, func(r *http.Request, c auth.Claims) map[string]any {
		return map[string]any{"Roles": c.Roles}
	})
}

// authorize is the shared core. It is no-op-safe (a == nil bypass) so
// BODHYANGA_AUTH=disabled doesn't have to remove the wrappers from the route
// table — main.go just passes a nil Authenticator.
func authorize(a *auth.Authenticator, module, query string, buildInput func(*http.Request, auth.Claims) map[string]any) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a == nil {
				next.ServeHTTP(w, r)
				return
			}
			claims, ok := Claims(r.Context())
			if !ok {
				// Authorize without Authenticate is a configuration bug;
				// the safest user-facing outcome is 401 (we can't gate
				// without claims, so we behave as if no token was sent).
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if err := a.Evaluate(r.Context(), module, query, buildInput(r, claims)); err != nil {
				if errors.Is(err, auth.ErrForbidden) {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				// Any other error path (e.g. Rego prepare failure) is a
				// 500 — the policy is mis-deployed, not the user's fault.
				http.Error(w, "authorization error", http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
