-- 0002_rls.sql
-- Row-Level Security policies for tenant isolation.
--
-- The contract: every query the application runs MUST first execute
--   SET LOCAL app.tenant_id = '<tenant-uuid>';
-- Then RLS allows rows matching tenant_id = current_setting('app.tenant_id').
-- Anything that forgets the SET will read zero rows (and, on insert, raise an
-- RLS violation) — failure-closed.
--
-- The owner role (typically a DBA/migration role) bypasses RLS by default;
-- the application role does not. The runtime store in this package opens
-- pgx.Pool as the application role and is bound by these policies.

ALTER TABLE interaction_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_records      ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events         ENABLE ROW LEVEL SECURITY;

-- FORCE applies the policy even to the table owner. Without this, running
-- migrations or psql as the owner would silently bypass tenancy. Enabling
-- FORCE means the owner must SET app.tenant_id too — which is the safer
-- default for HIPAA-style audits.
ALTER TABLE interaction_records FORCE ROW LEVEL SECURITY;
ALTER TABLE patient_records      FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_events         FORCE ROW LEVEL SECURITY;

-- ----------------------------------------------------------------------------
-- interaction_records
-- ----------------------------------------------------------------------------
DROP POLICY IF EXISTS interaction_records_tenant_isolation ON interaction_records;
CREATE POLICY interaction_records_tenant_isolation
    ON interaction_records
    USING       (tenant_id = current_setting('app.tenant_id', true))
    WITH CHECK  (tenant_id = current_setting('app.tenant_id', true));

-- ----------------------------------------------------------------------------
-- patient_records
-- ----------------------------------------------------------------------------
DROP POLICY IF EXISTS patient_records_tenant_isolation ON patient_records;
CREATE POLICY patient_records_tenant_isolation
    ON patient_records
    USING       (tenant_id = current_setting('app.tenant_id', true))
    WITH CHECK  (tenant_id = current_setting('app.tenant_id', true));

-- ----------------------------------------------------------------------------
-- audit_events  --  append-only
-- ----------------------------------------------------------------------------
--
-- Two separate policies so we can show explicit intent:
--   * SELECT  -- read your own tenant's events
--   * INSERT  -- write events for your own tenant
-- UPDATE and DELETE policies are intentionally absent. Combined with the
-- standard practice of GRANTing only SELECT, INSERT on this table to the
-- application role, the rows are append-only at the database layer.
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

-- ----------------------------------------------------------------------------
-- Application role
-- ----------------------------------------------------------------------------
--
-- Migrations run as a privileged role (typically the DB owner). The
-- application MUST connect as a separate, non-superuser role so the RLS
-- policies above actually apply — superusers and roles with BYPASSRLS
-- silently skip every policy.
--
-- This block creates bodhyanga_app if it does not exist, grants the minimum
-- privileges required, and explicitly STRIPS BYPASSRLS in case the role
-- pre-existed with that attribute (testcontainers postgres images often do).
--
-- Application connection string should be:
--   postgres://bodhyanga_app:<pass>@host:5432/bodhyanga
-- Migrations connection string remains the owner role.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'bodhyanga_app') THEN
        CREATE ROLE bodhyanga_app LOGIN PASSWORD 'bodhyanga_app' NOBYPASSRLS;
    ELSE
        ALTER ROLE bodhyanga_app NOBYPASSRLS;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO bodhyanga_app;

-- interaction_records, patient_records: full CRUD (RLS scopes by tenant).
GRANT SELECT, INSERT, UPDATE, DELETE ON interaction_records TO bodhyanga_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON patient_records      TO bodhyanga_app;

-- audit_events: append-only. UPDATE and DELETE are intentionally withheld.
GRANT SELECT, INSERT ON audit_events TO bodhyanga_app;

-- schema_migrations is read-only for the app; the migrator writes it.
GRANT SELECT ON schema_migrations TO bodhyanga_app;

INSERT INTO schema_migrations (version) VALUES (2) ON CONFLICT DO NOTHING;
