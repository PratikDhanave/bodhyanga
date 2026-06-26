package postgres_test

// Round-trip integration tests for the Postgres-backed hitl.ReviewStore
// (review_store.go + migrations/0005_reviews.sql). They use pkg/dbtest's
// shared testcontainers harness, so they only run where Docker is available.

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/dbtest"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

func sampleReview() medical.ReviewRequest {
	return medical.ReviewRequest{
		Kind:        "care_plan_review",
		PatientID:   "p-1",
		CaseID:      "c-1",
		Priority:    "high",
		Subject:     "Approve CAP care plan", // PHI-ish: must be encrypted at rest
		Detail:      "spirometry weekly",
		NextTo:      "engagement",
		NextType:    "engagement_request",
		Payload:     `{"plan":"original"}`,
		Meta:        map[string]any{"case_id": "c-1", "tenant_id": "t1"},
		SubmittedBy: "cdm_planner",
	}
}

// TestReviewStoreSubmitGet checks Submit fills ID/SubmittedAt/status and that
// every field — including the encrypted-payload PHI ones — survives a Get.
func TestReviewStoreSubmitGet(t *testing.T) {
	db := dbtest.NewDB(t)
	store := db.Store.ReviewStore(db.TenantID)

	got := store.Submit(sampleReview())
	if got.ID == "" {
		t.Fatal("Submit must assign an ID")
	}
	if got.SubmittedAt.IsZero() {
		t.Error("Submit must stamp SubmittedAt")
	}
	if got.Status != medical.ReviewPending {
		t.Errorf("Submit must force status pending, got %q", got.Status)
	}

	round, ok := store.Get(got.ID)
	if !ok {
		t.Fatal("Get returned ok=false for a just-submitted review")
	}
	if round.Subject != "Approve CAP care plan" || round.Detail != "spirometry weekly" {
		t.Errorf("encrypted Subject/Detail lost: %+v", round)
	}
	if round.NextTo != "engagement" || round.NextType != "engagement_request" || round.Payload != `{"plan":"original"}` {
		t.Errorf("resume-routing fields lost: %+v", round)
	}
	if round.Meta["case_id"] != "c-1" {
		t.Errorf("Meta lost: %+v", round.Meta)
	}
}

// TestReviewStoreDecideApproveResumes is the end-to-end decoupled HITL path:
// submit -> decide(approve) over Postgres -> BuildResumeMessage rebuilds the
// resume message addressed to NextTo, all without the agents package.
func TestReviewStoreDecideApproveResumes(t *testing.T) {
	db := dbtest.NewDB(t)
	store := db.Store.ReviewStore(db.TenantID)

	sub := store.Submit(sampleReview())
	decided, ok := store.Decide(sub.ID, "dr-foo", "approve", "looks good", "")
	if !ok {
		t.Fatalf("Decide(approve) returned ok=false")
	}
	if decided.Status != medical.ReviewApproved {
		t.Errorf("status = %q, want approved", decided.Status)
	}
	if decided.ReviewerID != "dr-foo" || decided.Reason != "looks good" || decided.DecidedAt.IsZero() {
		t.Errorf("decision fields not recorded: %+v", decided)
	}

	msg, resume := hitl.BuildResumeMessage(decided, "dr-foo", "")
	if !resume {
		t.Fatal("approved review must resume")
	}
	if msg.To != "engagement" || msg.Type != "engagement_request" || msg.Content != `{"plan":"original"}` {
		t.Errorf("resume message wrong: to=%q type=%q content=%q", msg.To, msg.Type, msg.Content)
	}
}

// TestReviewStoreDecideIdempotent confirms a second decide is a no-op (false),
// matching hitl.Queue.Decide's safety property against double-clicks.
func TestReviewStoreDecideIdempotent(t *testing.T) {
	db := dbtest.NewDB(t)
	store := db.Store.ReviewStore(db.TenantID)

	sub := store.Submit(sampleReview())
	if _, ok := store.Decide(sub.ID, "dr-foo", "approve", "", ""); !ok {
		t.Fatal("first Decide should succeed")
	}
	got, ok := store.Decide(sub.ID, "dr-bar", "reject", "changed mind", "")
	if ok {
		t.Error("second Decide must return ok=false (already decided)")
	}
	if got.Status != medical.ReviewApproved {
		t.Errorf("status must stay approved after a rejected re-decide, got %q", got.Status)
	}
}

// TestReviewStoreDecideInvalid confirms an unknown decision string is rejected
// without mutating the row.
func TestReviewStoreDecideInvalid(t *testing.T) {
	db := dbtest.NewDB(t)
	store := db.Store.ReviewStore(db.TenantID)

	sub := store.Submit(sampleReview())
	got, ok := store.Decide(sub.ID, "dr-foo", "yes", "", "")
	if ok {
		t.Error("invalid decision string must return ok=false")
	}
	if got.Status != medical.ReviewPending {
		t.Errorf("status must stay pending after invalid decision, got %q", got.Status)
	}
}

// TestReviewStorePending confirms Pending returns only undecided reviews.
func TestReviewStorePending(t *testing.T) {
	db := dbtest.NewDB(t)
	store := db.Store.ReviewStore(db.TenantID)

	keepOpen := store.Submit(sampleReview())
	toDecide := store.Submit(sampleReview())
	store.Decide(toDecide.ID, "dr-foo", "approve", "", "")

	pending := store.Pending()
	if len(pending) != 1 || pending[0].ID != keepOpen.ID {
		t.Fatalf("Pending = %d entries, want exactly the undecided one (%s)", len(pending), keepOpen.ID)
	}
	if all := store.List(); len(all) != 2 {
		t.Errorf("List should still show both reviews, got %d", len(all))
	}
}

// TestReviewStoreTenantIsolation confirms RLS hides one tenant's reviews from
// another.
func TestReviewStoreTenantIsolation(t *testing.T) {
	dbA := dbtest.NewDB(t)
	dbB := dbtest.NewDB(t)
	if dbA.TenantID == dbB.TenantID {
		t.Fatalf("two NewDB calls returned the same tenant: %s", dbA.TenantID)
	}
	ctx := context.Background()

	if _, err := dbA.Store.SubmitReview(ctx, dbA.TenantID, sampleReview()); err != nil {
		t.Fatalf("SubmitReview(A): %v", err)
	}
	bReviews, err := dbB.Store.ListReviews(ctx, dbB.TenantID)
	if err != nil {
		t.Fatalf("ListReviews(B): %v", err)
	}
	if len(bReviews) != 0 {
		t.Fatalf("tenant B saw tenant A's reviews: %+v", bReviews)
	}
}
