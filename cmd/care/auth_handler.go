// JWT authentication endpoints and middleware for cmd/care.
//
// Endpoints:
//
//	POST /api/auth/login   — exchange credentials for a token
//	POST /api/auth/refresh — exchange a valid token for a fresh one
//	GET  /api/auth/me      — return the authenticated user's profile
//
// Middleware:
//
//	authMiddleware wraps the mux so that every /api/* route except
//	POST /api/auth/login requires a valid Bearer token. Public routes
//	(/healthz, /readyz) bypass the middleware entirely because they are
//	registered before authMiddleware is applied.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/PratikDhanave/bodh/pkg/auth"
)

// contextKey is an unexported type for context keys in this package.
type contextKey int

const (
	// ctxUser is the context key used to store the authenticated *auth.User.
	ctxUser contextKey = iota
)

// tokenTTL is the lifetime of an issued credential token.
const tokenTTL = 8 * time.Hour

// publicRoutes lists path prefixes that do NOT require authentication. The
// check is prefix-based so /api/auth/login/extra is also public (callers
// won't reach the actual handler but we never want to 401 them before they
// even try to log in).
var publicRoutes = []string{
	"POST /api/auth/login",
	"GET /healthz",
	"GET /readyz",
}

// authMiddleware returns an HTTP middleware that validates Bearer tokens on
// all /api/* routes except those in publicRoutes. Public routes and
// non-/api/* routes pass through unchanged. On a valid token the
// *auth.User is stored in the request context under ctxUser.
func authMiddleware(store *auth.CredentialStore, secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only gate /api/* paths.
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			// Check whether this specific method+path is public.
			routeKey := r.Method + " " + r.URL.Path
			for _, pub := range publicRoutes {
				if strings.EqualFold(routeKey, pub) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Require Authorization: Bearer <token>.
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, "authorization required", http.StatusUnauthorized)
				return
			}
			rawToken := strings.TrimPrefix(authHeader, "Bearer ")
			u, err := store.ValidateToken(rawToken, secret)
			if err != nil {
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ctxUser, u)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// userFromContext extracts the authenticated *auth.User from ctx. Returns nil
// when the context carries no user (e.g. unauthenticated route).
func userFromContext(ctx context.Context) *auth.User {
	u, _ := ctx.Value(ctxUser).(*auth.User)
	return u
}

// authServer holds the credential store + JWT secret and exposes the three
// /api/auth/* handlers.
type authServer struct {
	store  *auth.CredentialStore
	secret string
}

// mountAuth registers the auth endpoints on mux.
func (a *authServer) mountAuth(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/refresh", a.refresh)
	mux.HandleFunc("GET /api/auth/me", a.me)
}

// login handles POST /api/auth/login.
//
// Request body:  {"email": "...", "password": "..."}
// Response body: {"token": "...", "expires_at": "RFC3339", "role": "..."}
func (a *authServer) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	u, err := a.store.Verify(body.Email, body.Password)
	if err != nil {
		// Return 401 for both "user not found" and "wrong password" to avoid
		// leaking which emails exist in the store.
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	tok, err := a.store.IssueToken(u, a.secret, tokenTTL)
	if err != nil {
		http.Error(w, "token issuance failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"token":      tok,
		"expires_at": time.Now().Add(tokenTTL).UTC().Format(time.RFC3339),
		"role":       u.Role,
	})
}

// refresh handles POST /api/auth/refresh.
//
// Request body:  {"token": "..."}
// Response body: {"token": "...", "expires_at": "RFC3339"}
func (a *authServer) refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	u, err := a.store.ValidateToken(body.Token, a.secret)
	if err != nil {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	tok, err := a.store.IssueToken(u, a.secret, tokenTTL)
	if err != nil {
		http.Error(w, "token issuance failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"token":      tok,
		"expires_at": time.Now().Add(tokenTTL).UTC().Format(time.RFC3339),
	})
}

// me handles GET /api/auth/me.
//
// Requires a valid Bearer token (enforced by authMiddleware).
// Response body: {"id": "...", "email": "...", "role": "..."}
func (a *authServer) me(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{
		"id":    u.ID,
		"email": u.Email,
		"role":  u.Role,
	})
}
