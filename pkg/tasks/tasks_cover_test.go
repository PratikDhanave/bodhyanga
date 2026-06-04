package tasks

import (
	"testing"
)

// --- UpdateStatus: unknown ID ---

func TestUpdateStatus_UnknownID(t *testing.T) {
	q := NewQueue()
	_, ok := q.UpdateStatus("nonexistent", StatusDone)
	if ok {
		t.Error("expected false for unknown task ID")
	}
}

// --- UpdateStatus: cancelled ---

func TestUpdateStatus_Cancelled(t *testing.T) {
	q := NewQueue()
	task := q.Create(NurseTask{
		Kind:      "refill",
		PatientID: "p1",
		Subject:   "test",
	})
	updated, ok := q.UpdateStatus(task.ID, StatusCancelled)
	if !ok {
		t.Fatal("expected true for known task ID")
	}
	if updated.Status != StatusCancelled {
		t.Errorf("Status = %q, want %q", updated.Status, StatusCancelled)
	}
}
