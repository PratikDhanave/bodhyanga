package agent

import "testing"

// TestNewMessage verifies the agent.NewMessage convenience constructor
// forwards its arguments to protocol.NewMessage and returns a Message that
// carries them verbatim.
func TestNewMessage(t *testing.T) {
	meta := map[string]any{"case_id": "case-123", "trace_id": "t-1"}
	m := NewMessage("intake", "supervisor", RoleUser, "presentation", `{"chief_complaint":"cough"}`, meta)

	if m.From != "intake" {
		t.Errorf("From = %q, want %q", m.From, "intake")
	}
	if m.To != "supervisor" {
		t.Errorf("To = %q, want %q", m.To, "supervisor")
	}
	if m.Role != RoleUser {
		t.Errorf("Role = %q, want %q", m.Role, RoleUser)
	}
	if m.Type != "presentation" {
		t.Errorf("Type = %q, want %q", m.Type, "presentation")
	}
	if m.Content != `{"chief_complaint":"cough"}` {
		t.Errorf("Content = %q", m.Content)
	}
	if m.Metadata["case_id"] != "case-123" {
		t.Errorf("Metadata[case_id] = %v, want %q", m.Metadata["case_id"], "case-123")
	}
}

// TestNewMessage_NilMetadata ensures a nil metadata map is accepted (the
// constructor must not panic on the common no-metadata call).
func TestNewMessage_NilMetadata(t *testing.T) {
	m := NewMessage("user", "intake", RoleUser, "ping", "", nil)
	if m.From != "user" || m.To != "intake" || m.Type != "ping" {
		t.Fatalf("unexpected message: %+v", m)
	}
}
