# Contract: Postgres Checkpoint Store (pkg/ckptpg)

Implements MAF `checkpoint.Store[json.RawMessage]` exactly; consumed via
`checkpoint.NewJSONManager(store)` + `inproc.Default.WithCheckpointing(mgr)`.

```go
func New(pool *pgxpool.Pool) *Store          // table per db/0001_checkpoints.sql

// CreateCheckpoint: INSERT row {uuid, sessionID, parent?.CheckpointID, data, now()};
// returns CheckpointInfo{CheckpointID, SessionID}. Never UPDATEs (append-only).
// RetrieveCheckpoint: SELECT data by (session_id, info.CheckpointID);
// missing row ⇒ error (never empty payload).
// RetrieveIndex: SELECT infos by session_id ordered by created_at ASC;
// withParent != nil ⇒ only children of that checkpoint (lineage filter).
```

**Guarantees**
- Append-only at the application layer; migration grants INSERT+SELECT only to the
  app role (bodh persistence discipline).
- `data` (jsonb) is PHI-bearing: never logged, never in errors, never in test
  assertions beyond byte-equality round-trips.
- Concurrent-safe via pgxpool; no cross-session interference (session_id scoping).
- Round-trip fidelity: bytes out == bytes in (jsonb normalization is acceptable only
  if MAF resume passes — verified by the integration test, not assumed).

**Integration test (exit criterion 3, testcontainers)**
1. Run canonical case with `-db`; stop consuming at `RequestInfoEvent` (pause).
2. Hard-stop the run/process context. Assert a checkpoint row exists.
3. New process/run: resume by session ID; assert the SAME ReviewRequest.ID is
   republished.
4. Deliver approval; assert care plan output, `case_finalized`, eval record, and
   audit-trail invariant 6 (continuity across lifetimes).
