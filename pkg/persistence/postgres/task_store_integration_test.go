package postgres_test

// Round-trip integration tests for the Postgres-backed tasks.TaskStore
// (task_store.go + migrations/0004_tasks.sql). They use pkg/dbtest's shared
// testcontainers harness, so they only run where Docker is available.

import (
	"context"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/dbtest"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

// TestTaskStoreRoundTrip exercises Create (with default-filling) + List
// through the TaskStoreAdapter, the same surface the workflow agents hold.
func TestTaskStoreRoundTrip(t *testing.T) {
	db := dbtest.NewDB(t)
	store := db.Store.TaskStore(db.TenantID)

	// Create with an empty ID/Status: the adapter must fill them like
	// tasks.Queue.Create does, and persist Subject/Detail inside the
	// encrypted payload (not a cleartext column).
	created := store.Create(tasks.NurseTask{
		Kind:      "refill",
		PatientID: "p-1",
		CaseID:    "c-1",
		Priority:  "normal",
		DueAt:     time.Now().Add(24 * time.Hour).UTC(),
		Subject:   "Refill review: lisinopril",
		Detail:    "auto-approve declined",
	})
	if created.ID == "" {
		t.Fatal("Create must assign an ID when none is given")
	}
	if created.Status != tasks.StatusOpen {
		t.Errorf("Create must default Status to open, got %q", created.Status)
	}
	if created.CreatedAt.IsZero() {
		t.Error("Create must stamp CreatedAt")
	}

	got := store.List()
	if len(got) != 1 {
		t.Fatalf("List returned %d tasks, want 1", len(got))
	}
	round := got[0]
	if round.ID != created.ID || round.Kind != "refill" || round.PatientID != "p-1" {
		t.Errorf("round-trip mismatch: %+v", round)
	}
	// Subject/Detail must survive the encrypted-payload round trip.
	if round.Subject != "Refill review: lisinopril" || round.Detail != "auto-approve declined" {
		t.Errorf("encrypted payload fields lost: Subject=%q Detail=%q", round.Subject, round.Detail)
	}
}

// TestTaskStoreListOrder confirms List returns tasks in DueAt-ascending order,
// matching tasks.Queue.List.
func TestTaskStoreListOrder(t *testing.T) {
	db := dbtest.NewDB(t)
	store := db.Store.TaskStore(db.TenantID)

	now := time.Now().UTC()
	store.Create(tasks.NurseTask{ID: "late", Kind: "inbox", DueAt: now.Add(48 * time.Hour)})
	store.Create(tasks.NurseTask{ID: "soon", Kind: "inbox", DueAt: now.Add(1 * time.Hour)})

	got := store.List()
	if len(got) != 2 {
		t.Fatalf("List returned %d tasks, want 2", len(got))
	}
	if got[0].ID != "soon" || got[1].ID != "late" {
		t.Errorf("List order = [%s, %s], want [soon, late]", got[0].ID, got[1].ID)
	}
}

// TestTaskStoreTenantIsolation confirms a task written under one tenant is
// invisible to another — the RLS policy from migrations/0004_tasks.sql.
func TestTaskStoreTenantIsolation(t *testing.T) {
	dbA := dbtest.NewDB(t)
	dbB := dbtest.NewDB(t)
	if dbA.TenantID == dbB.TenantID {
		t.Fatalf("two NewDB calls returned the same tenant: %s", dbA.TenantID)
	}
	ctx := context.Background()

	if err := dbA.Store.CreateTask(ctx, dbA.TenantID, tasks.NurseTask{
		ID: "only-a", Kind: "inbox", Status: tasks.StatusOpen,
	}); err != nil {
		t.Fatalf("CreateTask(A): %v", err)
	}

	bTasks, err := dbB.Store.ListTasks(ctx, dbB.TenantID)
	if err != nil {
		t.Fatalf("ListTasks(B): %v", err)
	}
	if len(bTasks) != 0 {
		t.Fatalf("tenant B saw tenant A's tasks: %+v", bTasks)
	}
}
