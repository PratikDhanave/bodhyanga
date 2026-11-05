// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/mid/authen.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga:
//   - stdlib net/http middleware (not the upstream ardanlabs/service web.MidFunc).
//   - No userbus / Basic-auth path: Bodhyanga does not manage credentials.
//   - ContextKey type is unexported; access via Claims(ctx) accessor.

package mid

import (
	"context"
	"errors"
	"net/http"

	"github.com/PratikDhanave/bodhyanga/pkg/auth"
)

// claimsKey is the unexported context key for Claims. Unexported so
// outside packages can't collide; access via the Claims accessor.
type claimsKey struct{}

// WithClaims puts claims into the context. Public so test helpers and
// adjacent middleware (Track A's logger) can decorate requests.
func WithClaims(ctx context.Context, c auth.Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// Claims pulls the Claims out of ctx. Returns zero-value + false when
// no claims are present (e.g. BODHYANGA_AUTH=disabled, or middleware not
// installed).
func Claims(ctx context.Context) (auth.Claims, bool) {
	v, ok := ctx.Value(claimsKey{}).(auth.Claims)
	return v, ok
}

// Authenticate is HTTP middleware that parses the Authorization header
// as a Bearer JWT, validates it via the supplied Authenticator, and
// stashes the parsed Claims in the request context. On failure it
// writes 401 and stops the chain.
//
// The middleware is no-op-safe: if a == nil (BODHYANGA_AUTH=disabled) the
// inner handler runs without any Claims context entry. This is the
// dev-mode escape hatch promised in the task spec.
func Authenticate(a *auth.Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a == nil {
				next.ServeHTTP(w, r)
				return
			}
			bearer := r.Header.Get("Authorization")
			claims, err := a.Parse(r.Context(), bearer)
			if err != nil {
				// All authentication failures collapse to 401. We do
				// NOT echo the underlying error in the body (it can
				// leak whether the kid is known, whether the iss is
				// right, etc.) — only a generic Unauthorized line.
				_ = err // intentionally not exposed to the client
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// errMissingClaims is returned by guard helpers when ctx has no Claims.
// Internal — the middleware itself returns 401, never this error.
var errMissingClaims = errors.New("no claims in request context")
