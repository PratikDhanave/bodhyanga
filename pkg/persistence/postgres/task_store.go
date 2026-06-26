package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/PratikDhanave/bodh/pkg/tasks"
)

// task_store.go backs the tasks.TaskStore interface with Postgres so the
// nurse-task queue can be shared across separately deployed agent containers
// (inbox_triage / prior_auth / tcm_coordinator / refill_manager) and the
// backend, instead of an in-process *tasks.Queue. The full NurseTask is
// encrypted into payload_ct; cleartext columns exist only for tenant-scoped
// queries and ordering (see migrations/0004_tasks.sql).

// CreateTask persists one nurse task under tenantID. The caller is expected to
// have already filled ID / CreatedAt / Status (TaskStoreAdapter.Create does
// this to match tasks.Queue.Create). Upsert-on-conflict keeps the call
// idempotent under at-least-once redelivery once the bus is durable.
func (s *Store) CreateTask(ctx context.Context, tenantID string, t tasks.NurseTask) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	payload, err := s.enc.Encrypt(raw)
	if err != nil {
		return fmt.Errorf("postgres: encrypt task payload: %w", err)
	}
	var dueAt *time.Time
	if !t.DueAt.IsZero() {
		d := t.DueAt
		dueAt = &d
	}
	return s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO nurse_tasks
				(tenant_id, task_id, kind, patient_id, case_id, priority, status, due_at, assigned_to, payload_ct)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (tenant_id, task_id) DO UPDATE SET
				kind        = EXCLUDED.kind,
				patient_id  = EXCLUDED.patient_id,
				case_id     = EXCLUDED.case_id,
				priority    = EXCLUDED.priority,
				status      = EXCLUDED.status,
				due_at      = EXCLUDED.due_at,
				assigned_to = EXCLUDED.assigned_to,
				payload_ct  = EXCLUDED.payload_ct
		`, tenantID, t.ID, t.Kind, nullText(t.PatientID), nullText(t.CaseID),
			t.Priority, string(t.Status), dueAt, nullText(t.AssignedTo), payload)
		return err
	})
}

// ListTasks returns every task visible to tenantID, ordered by due date then
// creation time — matching tasks.Queue.List's DueAt-ascending order (NULL due
// dates, i.e. zero-value DueAt, sort first as they do in memory).
func (s *Store) ListTasks(ctx context.Context, tenantID string) ([]tasks.NurseTask, error) {
	var out []tasks.NurseTask
	err := s.withTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT payload_ct FROM nurse_tasks
			ORDER BY due_at ASC NULLS FIRST, created_at ASC
		`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sealed []byte
			if err := rows.Scan(&sealed); err != nil {
				return err
			}
			payload, err := s.enc.Decrypt(sealed)
			if err != nil {
				return fmt.Errorf("postgres: decrypt task payload: %w", err)
			}
			var t tasks.NurseTask
			if err := json.Unmarshal(payload, &t); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// TaskStore returns a tenant-scoped adapter implementing tasks.TaskStore over
// Postgres. Drop-in for code that holds a *tasks.Queue.
func (s *Store) TaskStore(tenantID string) *TaskStoreAdapter {
	return &TaskStoreAdapter{s: s, tenantID: tenantID}
}

// TaskStoreAdapter exposes the tasks.TaskStore surface on the Postgres store.
// Method signatures match pkg/tasks so it is drop-in compatible with agents
// that hold a tasks.TaskStore.
type TaskStoreAdapter struct {
	s        *Store
	tenantID string
}

// Compile-time proof the adapter satisfies the interface agents depend on.
var _ tasks.TaskStore = (*TaskStoreAdapter)(nil)

// Create mirrors tasks.Queue.Create: it fills ID / CreatedAt / Status defaults,
// persists the task under the adapter's tenant, and returns the completed
// value. The persistence error is swallowed to satisfy the interface (use
// Store.CreateTask directly when you need it).
func (a *TaskStoreAdapter) Create(t tasks.NurseTask) tasks.NurseTask {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	if t.Status == "" {
		t.Status = tasks.StatusOpen
	}
	_ = a.s.CreateTask(context.Background(), a.tenantID, t)
	return t
}

// List mirrors tasks.Queue.List (ordered by due date).
func (a *TaskStoreAdapter) List() []tasks.NurseTask {
	out, _ := a.s.ListTasks(context.Background(), a.tenantID)
	return out
}

// nullText maps "" to a SQL NULL so optional columns stay null rather than
// empty string, matching the nullable schema in migrations/0004_tasks.sql.
func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
