// Copyright 2025 The Bodhyanga Authors
// Portions Copyright Ardan Labs, Bill Kennedy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Adapted from github.com/ardanlabs/service business/sdk/sqldb/dialect/postgres.go.

package dialect

import "bytes"

// Postgres is the Dialect for PostgreSQL. It uses the SQL:2008
// standard OFFSET / FETCH NEXT pagination form.
type Postgres struct{}

// Name implements Dialect.
func (Postgres) Name() string { return "postgres" }

// Paginate implements Dialect.
//
// The clause references :offset and :rows_per_page rather than literal
// bind variables; callers prepare a named parameter map and pass it
// through pgx's named-arg helper before exec.
func (Postgres) Paginate(buf *bytes.Buffer) {
	buf.WriteString(" OFFSET :offset ROWS FETCH NEXT :rows_per_page ROWS ONLY")
}
