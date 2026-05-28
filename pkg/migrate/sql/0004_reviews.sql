-- 0004_reviews.sql
-- HITL review queue tables.
--
-- review_requests is the durable mirror of pkg/hitl.Queue. A row is
-- inserted when an agent submits a clinically consequential message
-- for review and updated to "decided" when the reviewer approves /
-- modifies / rejects. The decision audit row lives in audit_events
-- (KindReviewDecision), so review_requests is the queue, not the
-- decision log.
--
-- payload_ct holds the gated message (PHI) pending the pgcrypto wrap.
-- routing columns above (patient_id / case_id / reviewer_id) are
-- correlation handles for the queue UI; they are NEVER MRNs.
--
-- The (tenant_id, status) index supports the "pending review" list
-- the clinician console polls; (tenant_id, submitted_at DESC) is
-- the audit-grade chronology used by the right-to-explanation
-- endpoint.

CREATE TABLE IF NOT EXISTS review_requests (
    tenant_id      TEXT        NOT NULL,
    review_id      TEXT        NOT NULL CHECK (review_id <> ''),
    case_id        TEXT,
    patient_id     TEXT,
    submitted_by   TEXT        NOT NULL,
    reviewer_id    TEXT,
    status         TEXT        NOT NULL DEFAULT 'pending',
    decision       TEXT,
    reason         TEXT,
    modification   TEXT,
    payload_ct     BYTEA       NOT NULL,
    submitted_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at     TIMESTAMPTZ,
    expires_at     TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, review_id)
);

CREATE INDEX IF NOT EXISTS review_requests_tenant_status_idx
    ON review_requests (tenant_id, status);
CREATE INDEX IF NOT EXISTS review_requests_tenant_submitted_idx
    ON review_requests (tenant_id, submitted_at DESC);
