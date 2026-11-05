-- Checkpoint store for MAF workflow state (specs/001 contracts/checkpoint-postgres.md).
-- APPEND-ONLY: the application role gets INSERT+SELECT only (bodhyanga persistence
-- discipline); data is the framework's opaque checkpoint payload and is
-- PHI-BEARING — never log it, never expose it outside this table's access tier.
CREATE TABLE IF NOT EXISTS checkpoints (
    seq        BIGSERIAL   PRIMARY KEY,
    id         UUID        NOT NULL UNIQUE,
    session_id TEXT        NOT NULL,
    parent_id  UUID        NULL REFERENCES checkpoints(id),
    data       JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS checkpoints_session_idx ON checkpoints (session_id, seq);

-- Example grants (role name deployment-specific):
--   GRANT SELECT, INSERT ON checkpoints TO bodhyanga_maf_app;
--   GRANT USAGE ON SEQUENCE checkpoints_seq_seq TO bodhyanga_maf_app;
