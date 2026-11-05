package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// migrationFS is the compiled-in copy of db/migrations. Files are named
// NNNN_description.sql; numeric prefix is the version. Versions are applied
// in ascending order and recorded in schema_migrations.
//
// NOTE: this directory is the embedded source of truth at runtime. The
// canonical copy operators reference via psql / a DBA workflow lives at
// db/migrations/ in the repo root; keep the two in sync (a CI check is a
// future small improvement). The embedded copy exists because Go's
// //go:embed cannot reach up the directory tree.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// "0001_init.sql" -> 1
		base := strings.SplitN(e.Name(), "_", 2)
		if len(base) == 0 {
			continue
		}
		v, err := strconv.Atoi(base[0])
		if err != nil {
			return nil, fmt.Errorf("migration %s: bad version prefix: %w", e.Name(), err)
		}
		b, err := fs.ReadFile(migrationFS, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		out = append(out, migration{version: v, name: e.Name(), sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// runMigrations applies any migration whose version is greater than the
// highest version recorded in schema_migrations. It is safe to call repeatedly
// — already-applied migrations are skipped (idempotent), and the SQL itself
// is written with IF NOT EXISTS / DROP POLICY IF EXISTS guards so a partial
// apply can be recovered by simply re-running.
//
// The runner uses a single transaction per migration so a failure halfway
// through one file rolls back to a clean state.
//
// Permission handling: the migrator is intended to run as the DB owner, but
// New() is also called by the application connecting as the bodhyanga_app role
// (which lacks DDL). When the schema is already up-to-date AND the calling
// role is read-only on schema_migrations, the runner is a no-op. Any actual
// pending migration that the app role cannot apply returns the underlying
// permission error so the operator sees it loudly.
func runMigrations(ctx context.Context, conn *pgx.Conn) error {
	migs, err := loadMigrations()
	if err != nil {
		return err
	}

	// Probe whether schema_migrations exists. If yes, we can read applied
	// versions even as a read-only role. If no, we need DDL to create it
	// (which is the owner's job).
	var hasTable bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = 'schema_migrations'
		)`).Scan(&hasTable); err != nil {
		return fmt.Errorf("probe schema_migrations: %w", err)
	}

	if !hasTable {
		if _, err := conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS schema_migrations (
				version    INTEGER PRIMARY KEY,
				applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`); err != nil {
			return fmt.Errorf("bootstrap schema_migrations: %w", err)
		}
	}

	applied := map[int]bool{}
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
		// The migration's own INSERT into schema_migrations is idempotent
		// (ON CONFLICT DO NOTHING) so we don't repeat it here, but we do
		// want to guarantee the row exists in case a migration forgets.
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`,
			m.version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record %s: %w", m.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", m.name, err)
		}
	}
	return nil
}
