// Package auth — simple credential store + HS256 token helpers.
//
// This file provides an in-memory user/password store with bcrypt hashing
// and HS256 JWT issuance. It is intentionally separate from the RS256
// Authenticator (auth.go) which is the production path for operator-issued
// tokens. The CredentialStore is the "demo / self-hosted" path: seed users
// are loaded at startup so the care API can accept username+password
// requests without an external IdP.
//
// Secret: BODHYANGA_JWT_SECRET (default "bodhyanga-dev-secret-change-in-prod").
package auth

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ErrUserNotFound is returned by Verify when the email is unknown.
var ErrUserNotFound = errors.New("user not found")

// ErrInvalidPassword is returned by Verify when the password does not match.
var ErrInvalidPassword = errors.New("invalid password")

// ErrInvalidCredentialToken is returned by ValidateToken when the HS256
// token is malformed, expired, or signed with a different secret.
var ErrInvalidCredentialToken = errors.New("invalid or expired credential token")

// DefaultJWTSecret is the fallback HS256 secret used when BODHYANGA_JWT_SECRET
// is not set. Never use the default in production.
const DefaultJWTSecret = "bodhyanga-dev-secret-change-in-prod"

// User is a Bodhyanga user record stored in a CredentialStore.
type User struct {
	ID           string // UUID
	Email        string
	Role         string
	PasswordHash string // bcrypt hash; not serialised to callers
}

// CredentialStore is an in-memory user store safe for concurrent use.
// Construct via NewCredentialStore.
type CredentialStore struct {
	mu    sync.RWMutex
	users map[string]User // keyed by email
}

// NewCredentialStore returns a CredentialStore pre-seeded with demo users:
//
//	admin/admin123     (role: admin)
//	clinician/clinic123 (role: clinician)
//
// The seeded credentials are illustrative only. Production deployments
// should replace them via AddUser before the server starts.
func NewCredentialStore() *CredentialStore {
	s := &CredentialStore{users: make(map[string]User)}

	seeds := []struct {
		email, password, role string
	}{
		{"admin", "admin123", RoleAdmin},
		{"clinician", "clinic123", RoleClinician},
	}
	for _, seed := range seeds {
		// Errors here are programming mistakes (bad bcrypt cost); panic fast.
		if err := s.AddUser(seed.email, seed.password, seed.role); err != nil {
			panic(fmt.Sprintf("auth: seed user %q: %v", seed.email, err))
		}
	}
	return s
}

// AddUser hashes password with bcrypt (cost 12) and stores a new User
// record keyed by email. Returns an error if bcrypt fails.
func (s *CredentialStore) AddUser(email, password, role string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	u := User{
		ID:           uuid.New().String(),
		Email:        email,
		Role:         role,
		PasswordHash: string(hash),
	}
	s.mu.Lock()
	s.users[email] = u
	s.mu.Unlock()
	return nil
}

// Verify looks up the user by email and compares password against the stored
// bcrypt hash. Returns ErrUserNotFound or ErrInvalidPassword on failure.
func (s *CredentialStore) Verify(email, password string) (*User, error) {
	s.mu.RLock()
	u, ok := s.users[email]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrUserNotFound
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidPassword
	}
	return &u, nil
}

// credentialClaims is the JWT claim set for HS256 credential tokens.
type credentialClaims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Role  string `json:"role"`
}

// IssueToken signs an HS256 JWT for u using secret, valid for ttl.
// Claims: sub=u.ID, email=u.Email, role=u.Role, iat=now, exp=now+ttl.
func (s *CredentialStore) IssueToken(u *User, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := credentialClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Email: u.Email,
		Role:  u.Role,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign hs256 token: %w", err)
	}
	return signed, nil
}

// ValidateToken parses and validates an HS256 JWT signed with secret.
// Returns the reconstructed User on success or ErrInvalidCredentialToken.
func (s *CredentialStore) ValidateToken(token, secret string) (*User, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	var claims credentialClaims
	tok, err := parser.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	if err != nil || !tok.Valid {
		return nil, ErrInvalidCredentialToken
	}
	u := &User{
		ID:    claims.Subject,
		Email: claims.Email,
		Role:  claims.Role,
	}
	return u, nil
}

// JWTSecret returns the active HS256 secret: BODHYANGA_JWT_SECRET env var if
// set, otherwise DefaultJWTSecret.
func JWTSecret() string {
	if v := os.Getenv("BODHYANGA_JWT_SECRET"); v != "" {
		return v
	}
	return DefaultJWTSecret
}
