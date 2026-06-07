package agent

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NewMessage tests
// ---------------------------------------------------------------------------

func TestNewMessage_FieldsPopulated(t *testing.T) {
	meta := map[string]any{"case_id": "abc-123", "trace_id": "t-1"}
	msg := NewMessage("intake", "diagnostician", RoleAgent, "goal", "diagnose patient", meta)

	if msg.From != "intake" {
		t.Errorf("From = %q, want %q", msg.From, "intake")
	}
	if msg.To != "diagnostician" {
		t.Errorf("To = %q, want %q", msg.To, "diagnostician")
	}
	if msg.Role != RoleAgent {
		t.Errorf("Role = %q, want %q", msg.Role, RoleAgent)
	}
	if msg.Type != "goal" {
		t.Errorf("Type = %q, want %q", msg.Type, "goal")
	}
	if msg.Content != "diagnose patient" {
		t.Errorf("Content = %q, want %q", msg.Content, "diagnose patient")
	}
	if msg.ID == "" {
		t.Error("ID should be a non-empty UUID")
	}
	if msg.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	// CreatedAt should be UTC.
	if msg.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt should be UTC, got %v", msg.CreatedAt.Location())
	}
	// CreatedAt should be recent (within the last 5 seconds).
	if time.Since(msg.CreatedAt) > 5*time.Second {
		t.Errorf("CreatedAt too old: %v", msg.CreatedAt)
	}
	// Metadata should be passed through.
	if msg.Metadata["case_id"] != "abc-123" {
		t.Errorf("Metadata[case_id] = %v, want abc-123", msg.Metadata["case_id"])
	}
	if msg.Metadata["trace_id"] != "t-1" {
		t.Errorf("Metadata[trace_id] = %v, want t-1", msg.Metadata["trace_id"])
	}
}

func TestNewMessage_NilMetadata(t *testing.T) {
	msg := NewMessage("user", "intake", RoleUser, "presentation", "headache", nil)
	if msg.Metadata != nil {
		t.Errorf("expected nil metadata, got %v", msg.Metadata)
	}
	if msg.From != "user" {
		t.Errorf("From = %q, want %q", msg.From, "user")
	}
}

func TestNewMessage_EmptyFields(t *testing.T) {
	msg := NewMessage("", "", RoleSystem, "", "", nil)
	if msg.ID == "" {
		t.Error("ID should still be set even with empty fields")
	}
	if msg.Role != RoleSystem {
		t.Errorf("Role = %q, want %q", msg.Role, RoleSystem)
	}
}

func TestNewMessage_UniqueIDs(t *testing.T) {
	msg1 := NewMessage("a", "b", RoleAgent, "t", "c", nil)
	msg2 := NewMessage("a", "b", RoleAgent, "t", "c", nil)
	if msg1.ID == msg2.ID {
		t.Error("each message should have a unique ID")
	}
}

// ---------------------------------------------------------------------------
// Role constants tests
// ---------------------------------------------------------------------------

func TestRoleConstants(t *testing.T) {
	tests := []struct {
		name string
		role MessageRole
		want string
	}{
		{"RoleUser", RoleUser, "user"},
		{"RoleSystem", RoleSystem, "system"},
		{"RoleAgent", RoleAgent, "agent"},
		{"RoleTool", RoleTool, "tool"},
		{"RoleObserver", RoleObserver, "observer"},
		{"RoleEvaluator", RoleEvaluator, "evaluator"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.role) != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.role, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Message type alias test — verify agent.Message and protocol.Message
// are the same type (the alias works).
// ---------------------------------------------------------------------------

func TestMessageAlias(t *testing.T) {
	// NewMessage returns a protocol.Message via the agent.Message alias.
	// If the alias is broken, this wouldn't compile.
	var msg Message = NewMessage("from", "to", RoleUser, "type", "content", nil)
	if msg.From != "from" {
		t.Errorf("From = %q, want %q", msg.From, "from")
	}
}

// ---------------------------------------------------------------------------
// NewMessage with various roles
// ---------------------------------------------------------------------------

func TestNewMessage_AllRoles(t *testing.T) {
	roles := []MessageRole{RoleUser, RoleSystem, RoleAgent, RoleTool, RoleObserver, RoleEvaluator}
	for _, role := range roles {
		msg := NewMessage("src", "dst", role, "test", "content", nil)
		if msg.Role != role {
			t.Errorf("NewMessage with role %q produced Role = %q", role, msg.Role)
		}
	}
}

func TestNewMessage_WithMetadata(t *testing.T) {
	meta := map[string]any{
		"tenant_id": "tenant-x",
		"case_id":   "case-y",
		"count":     42,
		"nested":    map[string]any{"key": "val"},
	}
	msg := NewMessage("a", "b", RoleAgent, "t", "c", meta)

	if msg.Metadata["tenant_id"] != "tenant-x" {
		t.Errorf("tenant_id = %v", msg.Metadata["tenant_id"])
	}
	if msg.Metadata["count"] != 42 {
		t.Errorf("count = %v", msg.Metadata["count"])
	}
	nested, ok := msg.Metadata["nested"].(map[string]any)
	if !ok {
		t.Fatal("nested metadata not preserved")
	}
	if nested["key"] != "val" {
		t.Errorf("nested.key = %v", nested["key"])
	}
}
