package tasks

import (
	"testing"
	"time"
)

func TestQueueLifecycle(t *testing.T) {
	q := NewQueue()
	a := q.Create(NurseTask{Kind: "refill", PatientID: "p1", DueAt: time.Now().Add(2 * time.Hour)})
	b := q.Create(NurseTask{Kind: "tcm_follow_up", PatientID: "p1", DueAt: time.Now().Add(time.Hour)})
	all := q.List()
	if len(all) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(all))
	}
	if all[0].ID != b.ID {
		t.Errorf("expected earlier-due task first")
	}
	if _, ok := q.UpdateStatus(a.ID, StatusDone); !ok {
		t.Errorf("UpdateStatus failed for existing id")
	}
	open := q.FilterOpen()
	if len(open) != 1 || open[0].ID != b.ID {
		t.Errorf("FilterOpen wrong: %+v", open)
	}
	kinds := q.CountByKind()
	if kinds["refill"] != 1 || kinds["tcm_follow_up"] != 1 {
		t.Errorf("CountByKind wrong: %+v", kinds)
	}
}
