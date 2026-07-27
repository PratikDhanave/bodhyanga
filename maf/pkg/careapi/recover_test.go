package careapi

import (
	"context"
	"testing"

	"github.com/microsoft/agent-framework-go/workflow"
)

// fakeIndex records which sessions were queried and returns a scripted
// checkpoint count per session.
type fakeIndex struct {
	counts  map[string]int
	queried []string
}

func (f *fakeIndex) RetrieveIndex(_ context.Context, sessionID string, _ *workflow.CheckpointInfo) ([]workflow.CheckpointInfo, error) {
	f.queried = append(f.queried, sessionID)
	infos := make([]workflow.CheckpointInfo, f.counts[sessionID])
	return infos, nil
}

func TestRecoverPending_RematerializesSessionsWithCheckpoints(t *testing.T) {
	store := &fakeIndex{counts: map[string]int{"s-live": 2, "s-empty": 0}}
	reg := NewRegistry()
	known := map[string]Entry{
		"s-live":  {CaseToken: "tok-live", Diagnosis: "acs", Confidence: 0.9},
		"s-empty": {CaseToken: "tok-empty"},
	}

	if err := RecoverPending(context.Background(), store, reg, known); err != nil {
		t.Fatal(err)
	}

	// s-live had checkpoints → restored as awaiting_review.
	got, ok := reg.BySession("s-live")
	if !ok {
		t.Fatal("s-live not recovered")
	}
	if got.Status != StatusAwaitingReview || got.Diagnosis != "acs" {
		t.Errorf("recovered entry = %+v", got)
	}
	// s-empty had no checkpoints → skipped.
	if _, ok := reg.BySession("s-empty"); ok {
		t.Error("s-empty had no checkpoints; must not be recovered")
	}
	if len(reg.Pending()) != 1 {
		t.Errorf("Pending = %d, want 1", len(reg.Pending()))
	}
}
