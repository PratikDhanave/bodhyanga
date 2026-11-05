-- 0001_init.sql
-- Bootstrap migration: schema_migrations + audit_events.
--
-- The schema_migrations table is also created by pkg/migrate's Up()
-- bootstrap step (so the runner can read the applied-set before any
-- migration files run). Repeating its CREATE here is intentional —
-- it keeps every migration file self-contained, so a one-shot psql
-- apply of 0001_init.sql against a brand-new DB still works.
--
-- audit_events is the immutable HIPAA §164.312(b) trail. The schema
-- intentionally matches pkg/audit.Event field-for-field; see
-- pkg/audit/recorder.go for the rationale on field selection
-- ("enough to reconstruct, never enough to leak").
--
-- This migration also writes the migration's own audit_events row
-- at the end, which is the §164.312(b) invariant the migrate
-- package enforces for every subsequent migration too.

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_events (
    tenant_id    TEXT        NOT NULL,
    id           TEXT        NOT NULL,
    at           TIMESTAMPTZ NOT NULL,
    kind         TEXT        NOT NULL,
    agent_id     TEXT,
    message_id   TEXT,
    from_id      TEXT,
    to_id        TEXT,
    type         TEXT,
    patient_id   TEXT,
    case_id      TEXT,
    trace_id     TEXT,
    decision     TEXT,
    reason       TEXT,
    reviewer_id  TEXT,
    error_text   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS audit_events_tenant_at_idx
    ON audit_events (tenant_id, at DESC);
CREATE INDEX IF NOT EXISTS audit_events_tenant_kind_idx
    ON audit_events (tenant_id, kind);

-- The migrate runner writes audit_events for every applied migration
-- inside its own transaction. 0001 is the cold-start case: the audit
-- table is created in this same transaction, so we also insert the
-- 0001 audit row here. Subsequent migrations rely on the runner's
-- own INSERT (see pkg/migrate/migrate.go applyOne).
INSERT INTO audit_events
    (tenant_id, id, at, kind, agent_id, reason)
VALUES
    ('_bodhyanga_system',
     'mig-bootstrap-0001',
     now(),
     'schema_migration',
     'bodhyanga-migrator',
     '0001_init.sql: init')
ON CONFLICT (tenant_id, id) DO NOTHING;
