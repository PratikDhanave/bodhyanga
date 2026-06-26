-- 0005_reviews.sql
-- hitl_reviews — durable backing for hitl.ReviewStore (the HITL review queue).
--
-- Design: the FULL medical.ReviewRequest is JSON-encoded and AES-256-GCM-sealed
-- into payload_ct. Only two NON-PHI lifecycle columns are kept in cleartext:
--   status        — drives Pending() filtering and the idempotent Decide()
--                   transition (Decide only acts on a 'pending' row)
--   submitted_at  — drives List()/Pending() ordering (FIFO by submission)
-- Everything reviewers and the resume path need that could be PHI — Subject,
-- Detail, Payload, Meta, Reason, Modification — stays encrypted in payload_ct.
-- (This is stricter than the legacy db/migrations review schema, which kept
-- reason/modification in cleartext.)
--
-- This lets the human_review agent and the backend share one review queue
-- across separately deployed containers via Postgres instead of an in-process
-- *hitl.Queue, while the backend resumes approved reviews using
-- hitl.BuildResumeMessage without importing agent code.

CREATE TABLE IF NOT EXISTS hitl_reviews (
    tenant_id     TEXT        NOT NULL,
    review_id     TEXT        NOT NULL CHECK (review_id <> ''),
    status        TEXT        NOT NULL DEFAULT 'pending',
    submitted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    payload_ct    BYTEA       NOT NULL,   -- AES-256-GCM JSON of the full ReviewRequest
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, review_id)
);

-- status index for the pending-inbox query; submitted_at index for ordering.
CREATE INDEX IF NOT EXISTS hitl_reviews_tenant_status_idx
    ON hitl_reviews (tenant_id, status);
CREATE INDEX IF NOT EXISTS hitl_reviews_tenant_submitted_idx
    ON hitl_reviews (tenant_id, submitted_at);

-- Row-Level Security: same failure-closed tenant isolation as patient_records.
ALTER TABLE hitl_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE hitl_reviews FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS hitl_reviews_tenant_isolation ON hitl_reviews;
CREATE POLICY hitl_reviews_tenant_isolation
    ON hitl_reviews
    USING       (tenant_id = current_setting('app.tenant_id', true))
    WITH CHECK  (tenant_id = current_setting('app.tenant_id', true));

-- Full CRUD for the application role; RLS scopes every statement by tenant.
-- (UPDATE is required: Decide() flips status + re-seals payload_ct in place.)
GRANT SELECT, INSERT, UPDATE, DELETE ON hitl_reviews TO bodh_app;

COMMENT ON COLUMN hitl_reviews.payload_ct IS 'AES-256-GCM encrypted JSON blob of the full medical.ReviewRequest. status/submitted_at remain cleartext for lifecycle queries.';

-- record the migration
INSERT INTO schema_migrations (version) VALUES (5) ON CONFLICT DO NOTHING;
