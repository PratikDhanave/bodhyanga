package auth

import (
	"errors"
	"testing"
	"time"
)

// TestLogin_AddVerifyRoundtrip adds a user and verifies the correct password.
func TestLogin_AddVerifyRoundtrip(t *testing.T) {
	s := &CredentialStore{users: make(map[string]User)}
	if err := s.AddUser("alice@example.com", "secret123", RoleClinician); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u, err := s.Verify("alice@example.com", "secret123")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if u.Email != "alice@example.com" {
		t.Errorf("email: got %q want %q", u.Email, "alice@example.com")
	}
	if u.Role != RoleClinician {
		t.Errorf("role: got %q want %q", u.Role, RoleClinician)
	}
	if u.ID == "" {
		t.Error("ID must not be empty")
	}
}

// TestLogin_IssueValidateRoundtrip issues a token and validates it.
func TestLogin_IssueValidateRoundtrip(t *testing.T) {
	s := &CredentialStore{users: make(map[string]User)}
	if err := s.AddUser("bob@example.com", "pw", RoleAdmin); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u, err := s.Verify("bob@example.com", "pw")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	const secret = "test-secret-for-unit"
	tok, err := s.IssueToken(u, secret, time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if tok == "" {
		t.Fatal("IssueToken returned empty string")
	}

	got, err := s.ValidateToken(tok, secret)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if got.Email != u.Email {
		t.Errorf("email: got %q want %q", got.Email, u.Email)
	}
	if got.Role != RoleAdmin {
		t.Errorf("role: got %q want %q", got.Role, RoleAdmin)
	}
	if got.ID != u.ID {
		t.Errorf("ID: got %q want %q", got.ID, u.ID)
	}
}

// TestLogin_InvalidPassword verifies that a wrong password returns ErrInvalidPassword.
func TestLogin_InvalidPassword(t *testing.T) {
	s := &CredentialStore{users: make(map[string]User)}
	if err := s.AddUser("carol@example.com", "rightpassword", RoleNurse); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	_, err := s.Verify("carol@example.com", "wrongpassword")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

// TestLogin_UnknownUser verifies that an unknown email returns ErrUserNotFound.
func TestLogin_UnknownUser(t *testing.T) {
	s := &CredentialStore{users: make(map[string]User)}
	_, err := s.Verify("nobody@example.com", "any")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

// TestLogin_ExpiredToken verifies that an expired token is rejected.
func TestLogin_ExpiredToken(t *testing.T) {
	s := &CredentialStore{users: make(map[string]User)}
	if err := s.AddUser("dave@example.com", "pw", RoleClinician); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u, err := s.Verify("dave@example.com", "pw")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	const secret = "test-secret-expiry"
	// Negative TTL -> expired immediately.
	tok, err := s.IssueToken(u, secret, -time.Minute)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	_, err = s.ValidateToken(tok, secret)
	if !errors.Is(err, ErrInvalidCredentialToken) {
		t.Fatalf("expected ErrInvalidCredentialToken for expired token, got %v", err)
	}
}

// TestLogin_WrongSecret verifies that a token signed with a different secret is rejected.
func TestLogin_WrongSecret(t *testing.T) {
	s := &CredentialStore{users: make(map[string]User)}
	if err := s.AddUser("eve@example.com", "pw", RoleClinician); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u, _ := s.Verify("eve@example.com", "pw")

	tok, _ := s.IssueToken(u, "secret-A", time.Hour)
	_, err := s.ValidateToken(tok, "secret-B")
	if !errors.Is(err, ErrInvalidCredentialToken) {
		t.Fatalf("expected ErrInvalidCredentialToken for wrong secret, got %v", err)
	}
}

// TestLogin_NewCredentialStore ensures the default seed users work.
func TestLogin_NewCredentialStore(t *testing.T) {
	s := NewCredentialStore()

	u, err := s.Verify("admin", "admin123")
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if u.Role != RoleAdmin {
		t.Errorf("admin role: got %q want %q", u.Role, RoleAdmin)
	}

	u, err = s.Verify("clinician", "clinic123")
	if err != nil {
		t.Fatalf("seed clinician: %v", err)
	}
	if u.Role != RoleClinician {
		t.Errorf("clinician role: got %q want %q", u.Role, RoleClinician)
	}
}
