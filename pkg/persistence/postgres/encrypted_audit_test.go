package postgres

// encrypted_audit_test.go exercises the ListAuditEvents encrypted-payload
// decrypt path (lines 574-585 of store.go) which requires an active
// Encryptor. This is an internal test (package postgres) so it can use
// requirePostgres directly, setting BODH_ENCRYPTION_KEY before the Store
// is created.

import (
	"context"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// requirePostgresWithEncryption is like requirePostgres but sets
// BODH_ENCRYPTION_KEY so the Store's encryptor is active.
func requirePostgresWithEncryption(t *testing.T) *Store {
	t.Helper()

	// A test-only 32-byte key, base64-encoded. Not used outside tests.
	const testKey = "LXioUY+kpZuBuAozTrYtscezDuzblhBTgI662ERMyYE="
	t.Setenv("BODH_ENCRYPTION_KEY", testKey)

	return requirePostgres(t)
}

func TestListAuditEvents_EncryptedPayload(t *testing.T) {
	s := requirePostgresWithEncryption(t)
	ctx := context.Background()
	const tenant = "tenant-enc"

	if !s.enc.IsEnabled() {
		t.Fatal("encryptor should be enabled for this test")
	}

	// Write several events with the encryptor active — payload_ct will be
	// a real AES-256-GCM sealed blob, not plaintext.
	events := []audit.Event{
		{
			ID:        "enc-ev-1",
			At:        time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
			Kind:      audit.KindMessage,
			AgentID:   "intake",
			MessageID: "msg-enc-1",
			From:      "patient",
			To:        "intake",
			Type:      "chief_complaint",
			PatientID: "pt-enc-1",
			CaseID:    "case-enc-1",
			TraceID:   "trace-enc-1",
		},
		{
			ID:        "enc-ev-2",
			At:        time.Date(2026, 6, 1, 10, 1, 0, 0, time.UTC),
			Kind:      audit.KindPolicyDecision,
			AgentID:   "orchestrator",
			MessageID: "msg-enc-2",
			CaseID:    "case-enc-1",
			Decision:  "allow",
			Reason:    "policy passed",
		},
		{
			ID:         "enc-ev-3",
			At:         time.Date(2026, 6, 1, 10, 2, 0, 0, time.UTC),
			Kind:       audit.KindReviewDecision,
			AgentID:    "hitl-gate",
			MessageID:  "msg-enc-3",
			CaseID:     "case-enc-1",
			Decision:   "approve",
			Reason:     "reviewer approved",
			ReviewerID: "dr-jones",
		},
	}

	for _, ev := range events {
		if err := s.RecordAuditEvent(ctx, tenant, ev); err != nil {
			t.Fatalf("record %s: %v", ev.ID, err)
		}
	}

	// List — this exercises the encrypted payload decrypt path.
	out, err := s.ListAuditEvents(ctx, tenant)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 events, got %d", len(out))
	}

	// Events should be in chronological order.
	for i := 1; i < len(out); i++ {
		if out[i].At.Before(out[i-1].At) {
			t.Fatalf("events not in chronological order at index %d", i)
		}
	}

	// Verify the decrypted payload round-trips correctly.
	var review audit.Event
	for _, e := range out {
		if e.ID == "enc-ev-3" {
			review = e
			break
		}
	}
	if review.ID == "" {
		t.Fatal("enc-ev-3 not found in output")
	}
	if review.Decision != "approve" {
		t.Fatalf("review decision not preserved: got %q", review.Decision)
	}
	if review.ReviewerID != "dr-jones" {
		t.Fatalf("reviewer ID not preserved: got %q", review.ReviewerID)
	}
	if review.Reason != "reviewer approved" {
		t.Fatalf("reason not preserved: got %q", review.Reason)
	}

	// Verify message event fields survived encrypt/decrypt.
	var msg audit.Event
	for _, e := range out {
		if e.ID == "enc-ev-1" {
			msg = e
			break
		}
	}
	if msg.From != "patient" || msg.To != "intake" || msg.Type != "chief_complaint" {
		t.Fatalf("message routing not preserved: %+v", msg)
	}
	if msg.PatientID != "pt-enc-1" || msg.CaseID != "case-enc-1" {
		t.Fatalf("correlation IDs not preserved: %+v", msg)
	}
}

// TestEncryptedPatientRoundTrip verifies the patient Upsert+Get+List path
// with encryption enabled. This covers the encrypt/decrypt calls in
// UpsertPatient, GetPatient, ListPatients, and FilterPatientsByCondition.
func TestEncryptedPatientRoundTrip(t *testing.T) {
	s := requirePostgresWithEncryption(t)
	ctx := context.Background()
	const tenant = "tenant-enc-pt"

	if !s.enc.IsEnabled() {
		t.Fatal("encryptor should be enabled for this test")
	}

	p := medical.PatientRecord{
		PatientID:  "enc-pt-1",
		Age:        65,
		Sex:        "M",
		Conditions: []string{"E11.9", "I10"},
		LastA1c:    9.2,
		RPMEnrolled: true,
	}
	if err := s.UpsertPatient(ctx, tenant, p); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, ok, err := s.GetPatient(ctx, tenant, "enc-pt-1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Age != 65 || got.Sex != "M" || got.LastA1c != 9.2 {
		t.Fatalf("encrypted patient not preserved: %+v", got)
	}

	all, err := s.ListPatients(ctx, tenant)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 patient, got %d", len(all))
	}

	filtered, err := s.FilterPatientsByCondition(ctx, tenant, "E11.9")
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered patient, got %d", len(filtered))
	}
}

// TestNewWithEmptyDSN covers the early error return in New.
func TestNewWithEmptyDSN(t *testing.T) {
	ctx := context.Background()
	_, err := New(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty DSN")
	}
}

// TestNewWithUnreachableHost covers the pool-open / ping failure branch in New.
func TestNewWithUnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Port 1 is almost certainly not running Postgres.
	_, err := New(ctx, "postgres://bodh:bodh@127.0.0.1:1/bodh?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

// TestNewWithInvalidDSN covers the DSN parse error branch in New.
func TestNewWithInvalidDSN(t *testing.T) {
	ctx := context.Background()
	_, err := New(ctx, "://not-a-valid-dsn")
	if err == nil {
		t.Fatal("expected error for invalid DSN")
	}
}

// TestPingNilReceiver covers the nil receiver branch in Ping.
func TestPingNilReceiver(t *testing.T) {
	var s *Store
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping on nil store should return nil, got: %v", err)
	}
}

// TestCloseNilReceiver covers the nil receiver branch in Close.
func TestCloseNilReceiver(t *testing.T) {
	var s *Store
	s.Close() // should not panic
}

// TestLoadMigrations verifies the embedded migration loading returns non-zero
// migrations and they are sorted by version.
func TestLoadMigrations_Sorted(t *testing.T) {
	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("expected at least one migration")
	}
	for i := 1; i < len(migs); i++ {
		if migs[i].version <= migs[i-1].version {
			t.Fatalf("migrations not sorted: %d <= %d", migs[i].version, migs[i-1].version)
		}
	}
	// Verify each migration has a non-empty SQL body.
	for _, m := range migs {
		if m.sql == "" {
			t.Fatalf("migration %s has empty SQL", m.name)
		}
	}
}
