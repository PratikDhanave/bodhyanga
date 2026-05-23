package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestNewMessage_PopulatesIDAndTimestamp(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	m := NewMessage("from", "to", RoleUser, "test", "hi", nil)
	after := time.Now().UTC().Add(time.Second)

	if m.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if m.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt")
	}
	if m.CreatedAt.Before(before) || m.CreatedAt.After(after) {
		t.Fatalf("CreatedAt %v outside expected window [%v, %v]", m.CreatedAt, before, after)
	}

	// IDs should differ across calls (UUID).
	m2 := NewMessage("from", "to", RoleUser, "test", "hi", nil)
	if m.ID == m2.ID {
		t.Fatalf("expected unique IDs, got duplicate %q", m.ID)
	}
}

func TestNewMessage_DefaultsRoleAndMetadata(t *testing.T) {
	m := NewMessage("u", "", "", "", "", nil)

	if m.From != "u" {
		t.Fatalf("From: got %q", m.From)
	}
	if m.To != "" {
		t.Fatalf("To: got %q, want empty", m.To)
	}
	if m.Role != "" {
		t.Fatalf("Role: got %q, want zero", m.Role)
	}
	if m.Metadata != nil {
		t.Fatalf("Metadata: got %v, want nil", m.Metadata)
	}
	if m.ID == "" {
		t.Fatal("expected non-empty ID even with empty inputs")
	}
}

func TestRoleConstants(t *testing.T) {
	roles := map[string]MessageRole{
		"RoleUser":      RoleUser,
		"RoleAgent":     RoleAgent,
		"RoleSystem":    RoleSystem,
		"RoleTool":      RoleTool,
		"RoleObserver":  RoleObserver,
		"RoleEvaluator": RoleEvaluator,
	}

	seen := make(map[MessageRole]string)
	for name, r := range roles {
		if string(r) == "" {
			t.Errorf("%s is empty", name)
		}
		if prev, ok := seen[r]; ok {
			t.Errorf("%s duplicates %s (value %q)", name, prev, r)
		}
		seen[r] = name
	}
}

func TestMessageJSONRoundTrip(t *testing.T) {
	orig := NewMessage(
		"sender",
		"recipient",
		RoleAgent,
		"plan",
		"step 1",
		map[string]any{"trace_id": "t-1", "weight": float64(2)},
	)

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.ID != orig.ID {
		t.Errorf("ID: got %q want %q", got.ID, orig.ID)
	}
	if got.From != orig.From {
		t.Errorf("From: got %q want %q", got.From, orig.From)
	}
	if got.To != orig.To {
		t.Errorf("To: got %q want %q", got.To, orig.To)
	}
	if got.Role != orig.Role {
		t.Errorf("Role: got %q want %q", got.Role, orig.Role)
	}
	if got.Type != orig.Type {
		t.Errorf("Type: got %q want %q", got.Type, orig.Type)
	}
	if got.Content != orig.Content {
		t.Errorf("Content: got %q want %q", got.Content, orig.Content)
	}
	if !got.CreatedAt.Equal(orig.CreatedAt) {
		t.Errorf("CreatedAt: got %v want %v", got.CreatedAt, orig.CreatedAt)
	}
	if !reflect.DeepEqual(got.Metadata, orig.Metadata) {
		t.Errorf("Metadata: got %#v want %#v", got.Metadata, orig.Metadata)
	}
}
