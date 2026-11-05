// Package checkpoint adds persist/restore to the HITL review queue.
// It serialises the pending queue to a JSON file on disk so that
// reviews survive process restarts.
//
// Usage:
//
//	q := hitl.NewQueue()
//	cp := hitl.NewCheckpointer(q, "/var/lib/bodhyanga/hitl-queue.json")
//	if err := cp.Restore(); err != nil { ... }  // on startup
//	defer cp.Flush()                              // on shutdown
//
//	// Periodic auto-save (every N writes):
//	cp.AutoFlush(10)  // flush every 10 mutations
package hitl

import (
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// CheckpointData is the JSON envelope written to disk.
// version allows future schema migrations without breaking existing files.
type CheckpointData struct {
	// Version is incremented when the schema changes.
	Version int `json:"version"`
	// Pending holds the snapshot of all ReviewRequests at flush time.
	// Only pending items are restored on startup (decided/expired items
	// are historical record and do not need to survive a restart).
	Pending []medical.ReviewRequest `json:"pending"`
	// SavedAt is the UTC timestamp when the checkpoint was written,
	// recorded as an RFC3339 string for human readability.
	SavedAt string `json:"saved_at"`
}

const checkpointVersion = 1

// Checkpointer wraps a Queue and provides Flush/Restore for durable state.
//
// Atomic write strategy: Flush writes to <path>.tmp first and then
// calls os.Rename, which is atomic on POSIX filesystems. A crash
// mid-write therefore leaves either the old checkpoint or the new one
// intact — never a partially written file.
type Checkpointer struct {
	queue      *Queue
	path       string
	writeCount atomic.Int64
}

// NewCheckpointer returns a Checkpointer that persists q to path.
// path is the target JSON file; its parent directory must already exist.
func NewCheckpointer(q *Queue, path string) *Checkpointer {
	return &Checkpointer{
		queue: q,
		path:  path,
	}
}

// Flush writes the current queue state to the JSON file atomically.
// It captures only the pending items — decided and expired requests are
// intentionally omitted: they carry PHI and do not need to be restored
// (the LOSSLESS-ON-REJECT guarantee in queue.go means non-pending items
// are already reflected in the audit log before they become non-pending).
//
// The write is atomic: the data is written to <path>.tmp, then renamed
// over <path>. A crash between the two syscalls leaves the old checkpoint
// intact.
func (cp *Checkpointer) Flush() error {
	pending := cp.queue.Pending()

	data := CheckpointData{
		Version: checkpointVersion,
		Pending: pending,
		SavedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("hitl checkpoint: marshal: %w", err)
	}

	tmpPath := cp.path + ".tmp"
	if err := os.WriteFile(tmpPath, b, 0o600); err != nil {
		return fmt.Errorf("hitl checkpoint: write tmp: %w", err)
	}
	// os.Rename is atomic on POSIX; on Windows it is not, but Bodhyanga targets Linux.
	if err := os.Rename(tmpPath, cp.path); err != nil {
		// Best-effort cleanup of the tmp file; ignore the removal error.
		_ = os.Remove(tmpPath)
		return fmt.Errorf("hitl checkpoint: rename: %w", err)
	}
	return nil
}

// Restore reads the checkpoint file and re-submits all pending items into
// the queue. It is idempotent: items already present in the queue (same ID)
// are skipped rather than duplicated.
//
// If the file does not exist, Restore returns nil — a missing checkpoint
// is not an error (it means a fresh start).
//
// Only items with status=pending in the checkpoint are re-hydrated.
// Decided or expired items from a previous run carry no actionable state
// for the new process.
func (cp *Checkpointer) Restore() error {
	b, err := os.ReadFile(cp.path)
	if os.IsNotExist(err) {
		// No checkpoint yet; this is normal on first startup.
		return nil
	}
	if err != nil {
		return fmt.Errorf("hitl checkpoint: read: %w", err)
	}

	var data CheckpointData
	if err := json.Unmarshal(b, &data); err != nil {
		return fmt.Errorf("hitl checkpoint: unmarshal: %w", err)
	}

	if data.Version != checkpointVersion {
		return fmt.Errorf("hitl checkpoint: unsupported version %d (expected %d)", data.Version, checkpointVersion)
	}

	for _, r := range data.Pending {
		// Skip if already present (idempotent restore).
		if _, exists := cp.queue.Get(r.ID); exists {
			continue
		}
		// Force status to pending regardless of what was serialised;
		// we only serialise pending items, but be defensive.
		r.Status = medical.ReviewPending
		// Submit preserves the existing ID and SubmittedAt because
		// they are already set (Submit only populates them when zero).
		cp.queue.Submit(r)
	}
	return nil
}

// AutoFlush installs a hook on the queue's Submit method so that Flush is
// called automatically every `every` calls to Submit. The first flush
// occurs after the first `every` submissions since AutoFlush was called.
//
// It must be called at most once per Checkpointer / Queue pair. Flush
// errors are silently dropped — the queue continues to operate correctly
// even if the checkpoint write fails (the in-memory state is authoritative
// for the current process lifetime; the checkpoint is a crash-recovery aid,
// not a primary data store).
//
// every must be >= 1; values <= 0 are treated as 1.
func (cp *Checkpointer) AutoFlush(every int64) {
	if every <= 0 {
		every = 1
	}
	cp.queue.submitHook = func() {
		n := cp.writeCount.Add(1)
		if n%every == 0 {
			_ = cp.Flush()
		}
	}
}

