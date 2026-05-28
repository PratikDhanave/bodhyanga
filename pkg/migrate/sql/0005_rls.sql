-- 0005_rls.sql
-- Row-Level Security policies + role grants for bodh_app + bodh_audit_app.
--
-- Two application roles are created:
--
--   bodh_app       -- general application role. CRUD on
--                     interaction_records / patient_records /
--                     case_state / review_requests. SELECT + INSERT
--                     on audit_events (the table is append-only at
--                     the GRANT layer).
--
--   bodh_audit_app -- dedicated audit-writer role. SELECT + INSERT
--                     on audit_events only. Lets the auditing path
--                     run under a separate role from the application
--                     so a compromise of the application credential
--                     can't update / delete past audit rows even if
--                     a future grant slip-up loosened UPDATE on
--                     audit_events for bodh_app. Defence in depth
--                     against §164.312(b) tampering.
--
-- Both roles are created with NOBYPASSRLS so the policies in this
-- migration actually apply. If a role pre-exists with BYPASSRLS
-- (testcontainers images sometimes do) we explicitly strip it.
--
-- The policies are idempotent: DROP POLICY IF EXISTS ... CREATE
-- POLICY. Existing deployments that already ran the legacy
-- pkg/persistence/postgres/migrations/0002_rls.sql see no behaviour
-- change here — the policies bind to the same app.tenant_id session
-- variable contract.

ALTER TABLE interaction_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_records      ENABLE ROW LEVEL SECURITY;
ALTER TABLE case_state           ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_requests      ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events         ENABLE ROW LEVEL SECURITY;

ALTER TABLE interaction_records FORCE ROW LEVEL SECURITY;
ALTER TABLE patient_records      FORCE ROW LEVEL SECURITY;
ALTER TABLE case_state           FORCE ROW LEVEL SECURITY;
ALTER TABLE review_requests      FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_events         FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS interaction_records_tenant_isolation ON interaction_records;
CREATE POLICY interaction_records_tenant_isolation
    ON interaction_records
    USING       (tenant_id = current_setting('app.tenant_id', true))
    WITH CHECK  (tenant_id = current_setting('app.tenant_id', true));

DROP POLICY IF EXISTS patient_records_tenant_isolation ON patient_records;
CREATE POLICY patient_records_tenant_isolation
    ON patient_records
    USING       (tenant_id = current_setting('app.tenant_id', true))
    WITH CHECK  (tenant_id = current_setting('app.tenant_id', true));

DROP POLICY IF EXISTS case_state_tenant_isolation ON case_state;
CREATE POLICY case_state_tenant_isolation
    ON case_state
    USING       (tenant_id = current_setting('app.tenant_id', true))
    WITH CHECK  (tenant_id = current_setting('app.tenant_id', true));

DROP POLICY IF EXISTS review_requests_tenant_isolation ON review_requests;
CREATE POLICY review_requests_tenant_isolation
    ON review_requests
    USING       (tenant_id = current_setting('app.tenant_id', true))
    WITH CHECK  (tenant_id = current_setting('app.tenant_id', true));

-- audit_events: separate SELECT + INSERT policies. UPDATE and DELETE
-- policies are intentionally absent. The bootstrap migration-audit
-- row inserted by 0001 carries tenant_id='_bodh_system' and is
-- visible only to a role that has SET app.tenant_id='_bodh_system'
-- — typically the operator running cmd/bodhadmin migrate.
DROP POLICY IF EXISTS audit_events_tenant_select ON audit_events;
CREATE POLICY audit_events_tenant_select
    ON audit_events
    FOR SELECT
    USING (tenant_id = current_setting('app.tenant_id', true));

DROP POLICY IF EXISTS audit_events_tenant_insert ON audit_events;
CREATE POLICY audit_events_tenant_insert
    ON audit_events
    FOR INSERT
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

-- bodh_app role
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'bodh_app') THEN
        CREATE ROLE bodh_app LOGIN PASSWORD 'bodh_app' NOBYPASSRLS;
    ELSE
        ALTER ROLE bodh_app NOBYPASSRLS;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO bodh_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON interaction_records TO bodh_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON patient_records      TO bodh_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON case_state           TO bodh_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON review_requests      TO bodh_app;
GRANT SELECT, INSERT                 ON audit_events         TO bodh_app;
GRANT SELECT                         ON schema_migrations    TO bodh_app;

-- bodh_audit_app role -- separate writer for audit_events.
-- This is the role pkg/audit.Recorder runs under once the audit
-- sink is wired through pkg/persistence/postgres in a future PR.
-- For now it's provisioned so deployments can rotate to two-role
-- mode without an additional migration.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'bodh_audit_app') THEN
        CREATE ROLE bodh_audit_app LOGIN PASSWORD 'bodh_audit_app' NOBYPASSRLS;
    ELSE
        ALTER ROLE bodh_audit_app NOBYPASSRLS;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO bodh_audit_app;
GRANT SELECT, INSERT ON audit_events     TO bodh_audit_app;
GRANT SELECT         ON schema_migrations TO bodh_audit_app;
