// Package sqldb is adapted from Ardan Labs' service template
// (https://github.com/ardanlabs/service), licensed Apache 2.0,
// copyright Ardan Labs / Bill Kennedy.
//
// What was adapted
//
//   - business/sdk/sqldb/sqldb.go: Open / StatusCheck / typed error
//     sentinels. Bodhyanga swaps sqlx.DB for *pgxpool.Pool and adds
//     ClassifyError to surface pgconn.PgError codes; the rest is
//     structurally faithful to the upstream.
//   - business/sdk/sqldb/tran.go: Beginner / CommitRollbacker
//     transactor seam. Bodhyanga returns pgx.Tx (not sqlx.Tx) and adds
//     WithTx + SetTenant — the latter is a Bodhyanga-specific extension
//     wiring the RLS app.tenant_id session variable inside the
//     transaction body.
//   - business/sdk/sqldb/dialect/: Dialect interface + Postgres
//     implementation reproduced almost verbatim (only the package
//     header changes).
//   - business/sdk/sqldb/dbarray/: pgx-friendly array Valuer helpers
//     trimmed to the single-dimension scalar subset Bodhyanga actually
//     uses. The upstream copied this code from github.com/lib/pq under
//     MIT — that nested attribution is preserved in the per-file
//     header.
//
// What we did NOT adapt
//
//   - The query helpers (NamedExecContext, QuerySlice, etc) are not
//     copied. Domain stores in Bodhyanga use pgx.Tx / pgxpool.Pool directly
//     with positional bind variables; the upstream's sqlx.Named layer
//     would force a rewrite of every store to no benefit.
//   - The OTel + foundation/logger plumbing is not copied. Bodhyanga runs
//     observability through pkg/observability and attaches spans at
//     the domain-store boundary rather than at this layer.
//
// See the root NOTICE file for the full attribution chain.
package sqldb
