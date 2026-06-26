-- 0004_tasks.sql
-- nurse_tasks — durable backing for tasks.TaskStore (the nurse-task queue).
--
-- Mirrors the patient_records design: the full tasks.NurseTask is JSON-encoded
-- and AES-256-GCM-sealed into payload_ct, while routing/status columns are kept
-- in cleartext for tenant-scoped queries and ordering. Subject/Detail are
-- potential PHI and live ONLY inside the encrypted payload — they are not
-- broken out into queryable columns.
--
-- This lets the inbox_triage / prior_auth / tcm_coordinator / refill_manager
-- agents and the backend share one task queue across separately deployed
-- containers via Postgres instead of a shared in-process *tasks.Queue.

CREATE TABLE IF NOT EXISTS nurse_tasks (
    tenant_id    TEXT        NOT NULL,
    task_id      TEXT        NOT NULL CHECK (task_id <> ''),
    kind         TEXT        NOT NULL DEFAULT '',
    patient_id   TEXT,
    case_id      TEXT,
    priority     TEXT        NOT NULL DEFAULT 'normal',
    status       TEXT        NOT NULL DEFAULT 'open',
    due_at       TIMESTAMPTZ,
    assigned_to  TEXT,
    payload_ct   BYTEA       NOT NULL,   -- AES-256-GCM JSON of the full NurseTask
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, task_id)
);

-- Ordering index for List (due date) and the common open-work filter.
CREATE INDEX IF NOT EXISTS nurse_tasks_tenant_due_idx
    ON nurse_tasks (tenant_id, due_at);
CREATE INDEX IF NOT EXISTS nurse_tasks_tenant_status_idx
    ON nurse_tasks (tenant_id, status);

-- Row-Level Security: same failure-closed tenant isolation as patient_records.
ALTER TABLE nurse_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE nurse_tasks FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS nurse_tasks_tenant_isolation ON nurse_tasks;
CREATE POLICY nurse_tasks_tenant_isolation
    ON nurse_tasks
    USING       (tenant_id = current_setting('app.tenant_id', true))
    WITH CHECK  (tenant_id = current_setting('app.tenant_id', true));

-- Full CRUD for the application role; RLS scopes every statement by tenant.
GRANT SELECT, INSERT, UPDATE, DELETE ON nurse_tasks TO bodh_app;

COMMENT ON COLUMN nurse_tasks.payload_ct IS 'AES-256-GCM encrypted JSON blob of the full tasks.NurseTask. Cleartext columns remain for SQL querying.';

-- record the migration
INSERT INTO schema_migrations (version) VALUES (4) ON CONFLICT DO NOTHING;
