// Package migrate is adapted from Ardan Labs' service template
// (https://github.com/ardanlabs/service), licensed Apache 2.0,
// copyright Ardan Labs / Bill Kennedy.
//
// What was adapted
//
//   - business/sdk/migrate/migrate.go: the Migrate(ctx, db) entry
//     point and the embed.FS pattern for shipping SQL files inside
//     the binary. Bodhyanga implements the runner inline instead of
//     delegating to ardanlabs/darwin so we can write per-migration
//     audit_events rows and stay free of an extra dep.
//   - business/sdk/migrate/sql/: the numeric-prefix SQL naming
//     convention. Bodhyanga's NNNN_description.sql files in pkg/migrate/sql
//     follow the same shape.
//
// What we added on top of the upstream
//
//   - Per-migration audit_events row (HIPAA §164.312(b)). The schema
//     change itself is auditable, atomic with the DDL apply.
//   - Deterministic audit IDs (sha256 of version+filename) so cross-
//     environment reconciliation lines up.
//   - Tolerance for the 0001 cold-start case where audit_events does
//     not yet exist at the moment its INSERT would run; 0001 is
//     expected to insert its own audit row at the end of its SQL.
//   - Bodhyanga-canonical migrations live in pkg/migrate/sql and are
//     wired into the binary via go:embed in this package; tests pass
//     their own fs.FS into Up so they can exercise failure paths
//     against a synthetic set.
//
// See the root NOTICE file for the full attribution chain.
package migrate
