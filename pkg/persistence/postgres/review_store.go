package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// review_store.go backs the hitl.ReviewStore interface with Postgres so the
// human_review agent and the backend can share one HITL review queue across
// separately deployed containers instead of an in-process *hitl.Queue. The
// full ReviewRequest is encrypted into payload_ct; only status/submitted_at
// are cleartext for lifecycle queries (see migrations/0005_reviews.sql).

// SubmitReview persists a new review request under tenantID, mirroring
// hitl.Queue.Submit: it assigns an ID and SubmittedAt when absent and forces
// Status to pending. Returns the completed request.
func (s *Store) SubmitReview(ctx context.Context, tenantID string, r medical.ReviewRequest) (medical.ReviewRequest, error) {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.SubmittedAt.IsZero() {
		r.SubmittedAt = time.Now().UTC()
	}
	r.Status = medical.ReviewPending

	sealed, err := s.sealReview(r)
	if err != nil {
		return r, err
	}
	err = s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `
			INSERT INTO hitl_reviews (tenant_id, review_id, status, submitted_at, payload_ct)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (tenant_id, review_id) DO UPDATE SET
				status       = EXCLUDED.status,
				submitted_at = EXCLUDED.submitted_at,
				payload_ct   = EXCLUDED.payload_ct
		`, tenantID, r.ID, string(r.Status), r.SubmittedAt, sealed)
		return e
	})
	return r, err
}

// DecideReview records a reviewer's choice, mirroring hitl.Queue.Decide
// exactly — including its safety properties:
//
//   - idempotency: only a row whose status is still 'pending' transitions; a
//     second decide returns (current, false). Enforced atomically with
//     SELECT ... FOR UPDATE inside the tenant transaction so concurrent
//     deciders cannot both win.
//   - input validation: an unrecognised decision string returns (current,
//     false) without mutating anything.
//   - unknown id returns (zero, false).
//
// On success it flips the cleartext status column AND re-seals payload_ct with
// the mutated record (new Status/ReviewerID/Reason/Modification/DecidedAt) so
// Get/List stay consistent with the lifecycle column.
func (s *Store) DecideReview(ctx context.Context, tenantID, id, reviewerID, decision, reason, modification string) (medical.ReviewRequest, bool, error) {
	var (
		out medical.ReviewRequest
		ok  bool
	)
	err := s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT payload_ct FROM hitl_reviews
			WHERE tenant_id = $1 AND review_id = $2
			FOR UPDATE
		`, tenantID, id).Scan(&sealed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // unknown id: out stays zero, ok stays false
		}
		if err != nil {
			return err
		}
		r, err := s.openReview(sealed)
		if err != nil {
			return err
		}
		out = r
		// Already decided (or expired): no-op, return the current record.
		if r.Status != medical.ReviewPending {
			return nil
		}
		next, valid := decisionToStatus(decision)
		if !valid {
			return nil // invalid decision string: out=current, ok=false
		}
		r.Status = next
		r.ReviewerID = reviewerID
		r.Reason = reason
		r.Modification = modification
		r.DecidedAt = time.Now().UTC()

		resealed, err := s.sealReview(r)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE hitl_reviews SET status = $3, payload_ct = $4
			WHERE tenant_id = $1 AND review_id = $2
		`, tenantID, id, string(r.Status), resealed); err != nil {
			return err
		}
		out, ok = r, true
		return nil
	})
	return out, ok, err
}

// GetReview returns the request for id under tenantID.
func (s *Store) GetReview(ctx context.Context, tenantID, id string) (medical.ReviewRequest, bool, error) {
	var (
		out medical.ReviewRequest
		ok  bool
	)
	err := s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT payload_ct FROM hitl_reviews WHERE tenant_id = $1 AND review_id = $2
		`, tenantID, id).Scan(&sealed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		r, err := s.openReview(sealed)
		if err != nil {
			return err
		}
		out, ok = r, true
		return nil
	})
	return out, ok, err
}

// ListReviews returns every review visible to tenantID, ordered by submission
// time (FIFO) — matching hitl.Queue.List.
func (s *Store) ListReviews(ctx context.Context, tenantID string) ([]medical.ReviewRequest, error) {
	return s.queryReviews(ctx, tenantID, `
		SELECT payload_ct FROM hitl_reviews ORDER BY submitted_at ASC
	`)
}

// PendingReviews returns only the pending reviews for tenantID, ordered by
// submission time — matching hitl.Queue.Pending.
func (s *Store) PendingReviews(ctx context.Context, tenantID string) ([]medical.ReviewRequest, error) {
	return s.queryReviews(ctx, tenantID, `
		SELECT payload_ct FROM hitl_reviews WHERE status = 'pending' ORDER BY submitted_at ASC
	`)
}

// queryReviews runs a payload_ct-selecting query and decrypts each row.
func (s *Store) queryReviews(ctx context.Context, tenantID, sql string) ([]medical.ReviewRequest, error) {
	var out []medical.ReviewRequest
	err := s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sql)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sealed []byte
			if err := rows.Scan(&sealed); err != nil {
				return err
			}
			r, err := s.openReview(sealed)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// sealReview JSON-encodes and encrypts a ReviewRequest for payload_ct.
func (s *Store) sealReview(r medical.ReviewRequest) ([]byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	sealed, err := s.enc.Encrypt(raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: encrypt review payload: %w", err)
	}
	return sealed, nil
}

// openReview decrypts and decodes a payload_ct blob into a ReviewRequest.
func (s *Store) openReview(sealed []byte) (medical.ReviewRequest, error) {
	raw, err := s.enc.Decrypt(sealed)
	if err != nil {
		return medical.ReviewRequest{}, fmt.Errorf("postgres: decrypt review payload: %w", err)
	}
	var r medical.ReviewRequest
	if err := json.Unmarshal(raw, &r); err != nil {
		return medical.ReviewRequest{}, err
	}
	return r, nil
}

// decisionToStatus maps a reviewer decision string to its terminal status,
// matching hitl.Queue.Decide. The second return is false for any unrecognised
// string so callers can reject bad input without mutating state.
func decisionToStatus(decision string) (medical.ReviewStatus, bool) {
	switch decision {
	case "approve":
		return medical.ReviewApproved, true
	case "approve_with_modifications":
		return medical.ReviewModified, true
	case "reject":
		return medical.ReviewRejected, true
	default:
		return "", false
	}
}

// ReviewStore returns a tenant-scoped adapter implementing hitl.ReviewStore
// over Postgres. Drop-in for code that holds a *hitl.Queue.
func (s *Store) ReviewStore(tenantID string) *ReviewStoreAdapter {
	return &ReviewStoreAdapter{s: s, tenantID: tenantID}
}

// ReviewStoreAdapter exposes the hitl.ReviewStore surface on the Postgres
// store. Method signatures match pkg/hitl so it is drop-in compatible with the
// human_review agent and the backend review handlers.
type ReviewStoreAdapter struct {
	s        *Store
	tenantID string
}

// Compile-time proof the adapter satisfies the interface its consumers depend on.
var _ hitl.ReviewStore = (*ReviewStoreAdapter)(nil)

// Submit mirrors hitl.Queue.Submit.
//
// The hitl.ReviewStore interface has no error return, so a persistence failure
// cannot be propagated to the human_review agent (it would receive a populated,
// success-shaped ReviewRequest that was never durably stored — audit finding
// F1). Until the interface is widened to return an error, we FAIL LOUD instead
// of silent: a dropped clinical review is logged at a greppable/alertable
// marker so an operator can detect and recover it. PHI (PatientID, Payload) is
// deliberately excluded from the log line; Kind is a non-PHI template string.
func (a *ReviewStoreAdapter) Submit(r medical.ReviewRequest) medical.ReviewRequest {
	out, err := a.s.SubmitReview(context.Background(), a.tenantID, r)
	if err != nil {
		log.Printf("HITL_PERSIST_FAILURE ReviewStoreAdapter.Submit: review NOT durably stored (kind=%q tenant=%q): %v",
			r.Kind, a.tenantID, err)
	}
	return out
}

// Decide mirrors hitl.Queue.Decide. Like Submit, a persistence failure cannot
// be propagated through the interface, so it is logged loudly (F2): a lost
// reviewer decision must not vanish silently. The review ID is a UUID, not PHI.
func (a *ReviewStoreAdapter) Decide(id, reviewerID, decision, reason, modification string) (medical.ReviewRequest, bool) {
	out, ok, err := a.s.DecideReview(context.Background(), a.tenantID, id, reviewerID, decision, reason, modification)
	if err != nil {
		log.Printf("HITL_PERSIST_FAILURE ReviewStoreAdapter.Decide: decision NOT durably applied (reviewID=%q tenant=%q): %v",
			id, a.tenantID, err)
	}
	return out, ok
}

// Get mirrors hitl.Queue.Get.
func (a *ReviewStoreAdapter) Get(id string) (medical.ReviewRequest, bool) {
	out, ok, _ := a.s.GetReview(context.Background(), a.tenantID, id)
	return out, ok
}

// List mirrors hitl.Queue.List.
func (a *ReviewStoreAdapter) List() []medical.ReviewRequest {
	out, _ := a.s.ListReviews(context.Background(), a.tenantID)
	return out
}

// Pending mirrors hitl.Queue.Pending.
func (a *ReviewStoreAdapter) Pending() []medical.ReviewRequest {
	out, _ := a.s.PendingReviews(context.Background(), a.tenantID)
	return out
}
