// Package ckptpg is a PostgreSQL implementation of MAF's
// checkpoint.Store[json.RawMessage] (contracts/checkpoint-postgres.md).
// Rows are append-only; the data column carries the framework's opaque —
// and PHI-bearing — checkpoint payload, so it is never logged and never
// appears in error messages.
package ckptpg

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/microsoft/agent-framework-go/workflow"
	"github.com/microsoft/agent-framework-go/workflow/checkpoint"
)

//go:embed schema.sql
var schemaSQL string

// Store implements checkpoint.Store[json.RawMessage] over pgx.
type Store struct {
	pool *pgxpool.Pool
}

var _ checkpoint.Store[json.RawMessage] = (*Store)(nil)

// New wraps an existing pool. The caller owns the pool's lifecycle.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// EnsureSchema applies db/0001_checkpoints.sql (idempotent). Deployments
// with managed migrations can skip this and run the file themselves.
func (s *Store) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, schemaSQL)
	if err != nil {
		return fmt.Errorf("ckptpg: ensure schema: %w", err)
	}
	return nil
}

// CreateCheckpoint appends one row and returns its info. Never updates.
func (s *Store) CreateCheckpoint(ctx context.Context, sessionID string, data json.RawMessage, parent *workflow.CheckpointInfo) (workflow.CheckpointInfo, error) {
	id := uuid.NewString()
	var parentID *string
	if parent != nil {
		parentID = &parent.CheckpointID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO checkpoints (id, session_id, parent_id, data) VALUES ($1, $2, $3, $4)`,
		id, sessionID, parentID, data)
	if err != nil {
		return workflow.CheckpointInfo{}, fmt.Errorf("ckptpg: create checkpoint for session %s: %w", sessionID, err)
	}
	return workflow.CheckpointInfo{SessionID: sessionID, CheckpointID: id}, nil
}

// RetrieveCheckpoint loads one row's payload; a missing row is an error,
// never an empty payload.
func (s *Store) RetrieveCheckpoint(ctx context.Context, sessionID string, info workflow.CheckpointInfo) (json.RawMessage, error) {
	var data json.RawMessage
	err := s.pool.QueryRow(ctx,
		`SELECT data FROM checkpoints WHERE session_id = $1 AND id = $2`,
		sessionID, info.CheckpointID).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("ckptpg: checkpoint %s not found in session %s", info.CheckpointID, sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("ckptpg: retrieve checkpoint %s: %w", info.CheckpointID, err)
	}
	return data, nil
}

// RetrieveIndex lists a session's checkpoints in creation order, optionally
// filtered to children of withParent (lineage / time-travel).
func (s *Store) RetrieveIndex(ctx context.Context, sessionID string, withParent *workflow.CheckpointInfo) ([]workflow.CheckpointInfo, error) {
	query := `SELECT id FROM checkpoints WHERE session_id = $1 ORDER BY seq ASC`
	args := []any{sessionID}
	if withParent != nil {
		query = `SELECT id FROM checkpoints WHERE session_id = $1 AND parent_id = $2 ORDER BY seq ASC`
		args = append(args, withParent.CheckpointID)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ckptpg: index for session %s: %w", sessionID, err)
	}
	defer rows.Close()

	var infos []workflow.CheckpointInfo
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("ckptpg: scan index row: %w", err)
		}
		infos = append(infos, workflow.CheckpointInfo{SessionID: sessionID, CheckpointID: id})
	}
	return infos, rows.Err()
}
