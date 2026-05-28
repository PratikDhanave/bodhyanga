// Package postgres is the production-grade backend for Bodh's eval.Store,
// medical.PatientRegistry, and audit.Recorder contracts.
//
// It is wire-compatible with the JSON FileStore in pkg/persistence and the
// in-memory implementations next to each interface, but adds:
//
//   - Pooled pgx/v5 connections.
//   - Multi-tenant isolation via Postgres row-level security. Every query
//     SET LOCALs app.tenant_id so the policy in db/migrations/0002_rls.sql
//     can decide whether the caller is allowed to see / touch the row.
//   - An append-only audit table backed by a CREATE POLICY that omits any
//     UPDATE / DELETE rule (the application role should be GRANTed only
//     SELECT, INSERT — defence in depth alongside the RLS policy).
//
// PHI columns are typed as bytea today (pkg writes plaintext JSON bytes) so
// a follow-up PR can wire pgcrypto / KMS envelope encryption without a schema
// migration. See the comments in 0001_init.sql.
//
// The file/in-memory backends in pkg/persistence and pkg/audit are still the
// supported demo path; cmd/care selects the Postgres backend only when
// BODH_DB_DSN is set.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// Store is the multi-tenant PostgreSQL backend.
//
// One *Store fronts a single pgxpool.Pool. Every read/write takes a tenantID
// string. Tenant scoping is enforced by Postgres row-level security; the
// tenantID is propagated into the session via SET LOCAL app.tenant_id inside
// a one-statement transaction (a "session" in pgx terms is one pooled conn,
// and SET LOCAL is scoped to the surrounding tx).
type Store struct {
	pool *pgxpool.Pool

	// cachedTenant lets the eval/audit recorder-style adapters (which don't
	// take a tenant in their interface) still know which tenant to scope to.
	// Tests that want per-call routing should use the tenant-aware methods
	// directly; the adapters are convenience wrappers for cmd/care.
	mu            sync.RWMutex
	defaultTenant string
}

// New opens a pool against dsn, pings, runs migrations, and returns a ready
// Store. The caller owns the lifecycle and must call Close.
//
// dsn is a standard pgx/libpq connection string, e.g.:
//
//	postgres://bodh:bodh@localhost:5432/bodh?sslmode=disable
//
// Migrations are applied with a single dedicated connection acquired from
// the pool. Failures abort startup — the demo backend is not a silent
// fallback at this layer (cmd/care picks the backend at boot, not at runtime).
func New(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		return nil, errors.New("postgres: empty DSN")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}
	// Sensible defaults; operators can override via DSN params.
	if cfg.MaxConns == 0 {
		cfg.MaxConns = 8
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	// Run migrations on a dedicated connection so SET-and-back doesn't leak.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: acquire migration conn: %w", err)
	}
	migrationErr := runMigrations(ctx, conn.Conn())
	conn.Release()
	if migrationErr != nil {
		pool.Close()
		return nil, migrationErr
	}

	return &Store{pool: pool}, nil
}

// Close releases the underlying pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// Ping performs a cheap round-trip to Postgres. It is called by the cmd/care
// /readyz handler to confirm the database is reachable; it is not called on
// the hot path. A nil receiver is always healthy (in-memory path).
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return nil
	}
	return s.pool.Ping(ctx)
}

// CurrentRoleHasBypassRLS reports whether the connected role would silently
// skip every row-level-security policy. The application role MUST return
// false; if a test sees true, RLS is not being enforced and any subsequent
// tenant-isolation assertion is a false positive.
//
// Exposed as a public method on Store so test packages outside the
// postgres package (e.g. pkg/dbtest) can re-use the same invariant check
// without reaching into the unexported pool. The query is read-only and
// runs outside any tenant-scoped transaction.
func (s *Store) CurrentRoleHasBypassRLS(ctx context.Context) (bool, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	var bypass bool
	if err := conn.QueryRow(ctx,
		`SELECT rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&bypass); err != nil {
		return false, err
	}
	return bypass, nil
}

// SetDefaultTenant configures the tenant used by interface-conforming
// adapters that can't accept a tenantID parameter (e.g. eval.Store.Save).
// Pass an empty string to clear it.
func (s *Store) SetDefaultTenant(tenantID string) {
	s.mu.Lock()
	s.defaultTenant = tenantID
	s.mu.Unlock()
}

// DefaultTenant returns the tenant ID set via SetDefaultTenant.
func (s *Store) DefaultTenant() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.defaultTenant
}

// withTenant runs fn inside a transaction with app.tenant_id set so RLS
// policies see the caller's tenant. Every read/write in this package goes
// through this seam so a forgotten SET cannot leak rows.
func (s *Store) withTenant(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error {
	if tenantID == "" {
		return errors.New("postgres: tenantID required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	// set_config(name, value, is_local=true) is equivalent to SET LOCAL but
	// accepts a parameter, which lets us bind tenantID safely.
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return fmt.Errorf("postgres: set tenant: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// =============================================================================
// eval.Store — tenant-aware extension
// =============================================================================

// SaveInteraction inserts an eval record scoped to tenantID. The record's ID
// is the tenant-local key (PK is (tenant_id, id) so two tenants may reuse the
// same ID without colliding).
func (s *Store) SaveInteraction(ctx context.Context, tenantID string, r eval.InteractionRecord) error {
	metrics, err := json.Marshal(emptyMap(r.Metrics))
	if err != nil {
		return err
	}
	meta, err := json.Marshal(emptyAnyMap(r.Metadata))
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO interaction_records
				(tenant_id, id, scenario, success, metrics, metadata, started_at, ended_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (tenant_id, id) DO UPDATE SET
				scenario   = EXCLUDED.scenario,
				success    = EXCLUDED.success,
				metrics    = EXCLUDED.metrics,
				metadata   = EXCLUDED.metadata,
				started_at = EXCLUDED.started_at,
				ended_at   = EXCLUDED.ended_at
		`, tenantID, r.ID, r.Scenario, r.Success, metrics, meta, r.StartedAt, r.EndedAt)
		return err
	})
}

// ListInteractions returns every eval record visible to tenantID, ordered
// newest first.
func (s *Store) ListInteractions(ctx context.Context, tenantID string) ([]eval.InteractionRecord, error) {
	var out []eval.InteractionRecord
	err := s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, scenario, success, metrics, metadata, started_at, ended_at
			FROM interaction_records
			ORDER BY created_at DESC
		`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				rec     eval.InteractionRecord
				metrics []byte
				meta    []byte
			)
			if err := rows.Scan(&rec.ID, &rec.Scenario, &rec.Success, &metrics, &meta, &rec.StartedAt, &rec.EndedAt); err != nil {
				return err
			}
			if len(metrics) > 0 {
				_ = json.Unmarshal(metrics, &rec.Metrics)
			}
			if len(meta) > 0 {
				_ = json.Unmarshal(meta, &rec.Metadata)
			}
			out = append(out, rec)
		}
		return rows.Err()
	})
	return out, err
}

// EvalStore returns an eval.Store adapter bound to tenantID. Useful for
// passing into existing code that takes the bare interface.
func (s *Store) EvalStore(tenantID string) eval.Store {
	return &evalAdapter{s: s, tenantID: tenantID}
}

type evalAdapter struct {
	s        *Store
	tenantID string
}

func (a *evalAdapter) Save(r eval.InteractionRecord) error {
	return a.s.SaveInteraction(context.Background(), a.tenantID, r)
}

func (a *evalAdapter) List() []eval.InteractionRecord {
	recs, err := a.s.ListInteractions(context.Background(), a.tenantID)
	if err != nil {
		return nil
	}
	return recs
}

// =============================================================================
// medical.PatientRegistry — tenant-aware extension
// =============================================================================

// UpsertPatient inserts or replaces a patient record for tenantID. The full
// medical.PatientRecord is JSON-encoded into payload_ct (bytea) so a future
// pgcrypto/KMS PR can wrap it as ciphertext without changing the schema.
func (s *Store) UpsertPatient(ctx context.Context, tenantID string, p medical.PatientRecord) error {
	payload, err := json.Marshal(p)
	if err != nil {
		return err
	}
	var dischargedAt *time.Time
	if !p.DischargedAt.IsZero() {
		t := p.DischargedAt
		dischargedAt = &t
	}
	conditions := p.Conditions
	if conditions == nil {
		conditions = []string{}
	}
	return s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO patient_records
				(tenant_id, patient_id, conditions, rpm_enrolled, discharged_at, payload_ct)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (tenant_id, patient_id) DO UPDATE SET
				conditions    = EXCLUDED.conditions,
				rpm_enrolled  = EXCLUDED.rpm_enrolled,
				discharged_at = EXCLUDED.discharged_at,
				payload_ct    = EXCLUDED.payload_ct,
				updated_at    = now()
		`, tenantID, p.PatientID, conditions, p.RPMEnrolled, dischargedAt, payload)
		return err
	})
}

// GetPatient returns the record for patientID under tenantID. ok=false when
// the row does not exist OR is hidden by RLS (caller cannot distinguish).
func (s *Store) GetPatient(ctx context.Context, tenantID, patientID string) (medical.PatientRecord, bool, error) {
	var (
		rec medical.PatientRecord
		ok  bool
	)
	err := s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var payload []byte
		err := tx.QueryRow(ctx, `
			SELECT payload_ct FROM patient_records
			WHERE tenant_id = $1 AND patient_id = $2
		`, tenantID, patientID).Scan(&payload)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(payload, &rec); err != nil {
			return err
		}
		ok = true
		return nil
	})
	return rec, ok, err
}

// ListPatients returns every patient visible to tenantID.
func (s *Store) ListPatients(ctx context.Context, tenantID string) ([]medical.PatientRecord, error) {
	var out []medical.PatientRecord
	err := s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT payload_ct FROM patient_records ORDER BY created_at DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var payload []byte
			if err := rows.Scan(&payload); err != nil {
				return err
			}
			var rec medical.PatientRecord
			if err := json.Unmarshal(payload, &rec); err != nil {
				return err
			}
			out = append(out, rec)
		}
		return rows.Err()
	})
	return out, err
}

// FilterPatientsByCondition returns patients in tenantID whose Conditions
// include code. The condition column is indexed (GIN) so this scales beyond
// the demo PatientRegistry's in-memory scan.
func (s *Store) FilterPatientsByCondition(ctx context.Context, tenantID, code string) ([]medical.PatientRecord, error) {
	var out []medical.PatientRecord
	err := s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT payload_ct FROM patient_records
			WHERE $1 = ANY(conditions)
			ORDER BY created_at DESC
		`, code)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var payload []byte
			if err := rows.Scan(&payload); err != nil {
				return err
			}
			var rec medical.PatientRecord
			if err := json.Unmarshal(payload, &rec); err != nil {
				return err
			}
			out = append(out, rec)
		}
		return rows.Err()
	})
	return out, err
}

// DeletePatient hard-deletes the record. GDPR Article 17 ("right to be
// forgotten") and HIPAA §164.530(j) require an enforceable purge path; this
// is the seam operators wire into their data-subject-request runbook.
//
// Note this does NOT cascade through audit_events: the audit log keeps the
// pseudonymous patient_id (an opaque token, never an MRN) so the trail
// remains intact for compliance investigations. If the operator's threat
// model requires audit purge too, they can issue a separate scoped DELETE
// as the migrations role (which can override the audit append-only grant).
func (s *Store) DeletePatient(ctx context.Context, tenantID, patientID string) error {
	return s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			DELETE FROM patient_records
			WHERE tenant_id = $1 AND patient_id = $2
		`, tenantID, patientID)
		return err
	})
}

// PatientRegistry returns a *medical.PatientRegistry-shaped adapter bound to
// tenantID. The adapter satisfies the same Upsert/Get/List/FilterByCondition
// methods used by the in-memory implementation in pkg/medical.
func (s *Store) PatientRegistry(tenantID string) *PatientRegistryAdapter {
	return &PatientRegistryAdapter{s: s, tenantID: tenantID}
}

// PatientRegistryAdapter exposes the in-memory PatientRegistry surface on
// top of the Postgres store. Method signatures match pkg/medical so it is
// drop-in compatible for code that holds a *PatientRegistryAdapter.
type PatientRegistryAdapter struct {
	s        *Store
	tenantID string
}

// Upsert mirrors medical.PatientRegistry.Upsert. Errors are swallowed to
// preserve the interface; use UpsertPatient on Store when you need them.
func (a *PatientRegistryAdapter) Upsert(p medical.PatientRecord) {
	_ = a.s.UpsertPatient(context.Background(), a.tenantID, p)
}

// Get mirrors medical.PatientRegistry.Get.
func (a *PatientRegistryAdapter) Get(id string) (medical.PatientRecord, bool) {
	rec, ok, _ := a.s.GetPatient(context.Background(), a.tenantID, id)
	return rec, ok
}

// List mirrors medical.PatientRegistry.List.
func (a *PatientRegistryAdapter) List() []medical.PatientRecord {
	out, _ := a.s.ListPatients(context.Background(), a.tenantID)
	return out
}

// FilterByCondition mirrors medical.PatientRegistry.FilterByCondition.
func (a *PatientRegistryAdapter) FilterByCondition(code string) []medical.PatientRecord {
	out, _ := a.s.FilterPatientsByCondition(context.Background(), a.tenantID, code)
	return out
}

// =============================================================================
// audit.Recorder — tenant-aware extension
// =============================================================================

// RecordAuditEvent appends one row to audit_events under tenantID. The
// RLS policy + GRANT SELECT, INSERT enforce append-only semantics; if you
// ever see this method behind an UPDATE statement, the migration grants are
// wrong.
func (s *Store) RecordAuditEvent(ctx context.Context, tenantID string, e audit.Event) error {
	if e.ID == "" {
		return errors.New("postgres: audit event missing ID")
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	return s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO audit_events
				(tenant_id, id, at, kind, agent_id, message_id, from_id, to_id,
				 type, patient_id, case_id, trace_id, decision, reason,
				 reviewer_id, error_text)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		`,
			tenantID, e.ID, e.At, string(e.Kind), e.AgentID, e.MessageID,
			e.From, e.To, e.Type, e.PatientID, e.CaseID, e.TraceID,
			e.Decision, e.Reason, e.ReviewerID, e.ErrorText)
		return err
	})
}

// ListAuditEvents returns all events for tenantID ordered chronologically.
func (s *Store) ListAuditEvents(ctx context.Context, tenantID string) ([]audit.Event, error) {
	var out []audit.Event
	err := s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, at, kind, agent_id, message_id, from_id, to_id,
			       type, patient_id, case_id, trace_id, decision, reason,
			       reviewer_id, error_text
			FROM audit_events
			ORDER BY at ASC
		`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				e    audit.Event
				kind string
			)
			if err := rows.Scan(&e.ID, &e.At, &kind, &e.AgentID, &e.MessageID,
				&e.From, &e.To, &e.Type, &e.PatientID, &e.CaseID,
				&e.TraceID, &e.Decision, &e.Reason, &e.ReviewerID, &e.ErrorText); err != nil {
				return err
			}
			e.Kind = audit.Kind(kind)
			e.TenantID = "" // RLS gave us our tenant; don't echo it back
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// AuditRecorder returns an audit.Recorder adapter bound to tenantID.
func (s *Store) AuditRecorder(tenantID string) audit.Recorder {
	return &auditAdapter{s: s, tenantID: tenantID}
}

type auditAdapter struct {
	s        *Store
	tenantID string
}

func (a *auditAdapter) Record(e audit.Event) error {
	return a.s.RecordAuditEvent(context.Background(), a.tenantID, e)
}

func (a *auditAdapter) List() []audit.Event {
	out, _ := a.s.ListAuditEvents(context.Background(), a.tenantID)
	return out
}

// =============================================================================
// helpers
// =============================================================================

func emptyMap(m map[string]float64) map[string]float64 {
	if m == nil {
		return map[string]float64{}
	}
	return m
}

func emptyAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
