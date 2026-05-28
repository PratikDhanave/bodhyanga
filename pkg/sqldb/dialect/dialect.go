// Copyright 2025 The Bodh Authors
// Portions Copyright Ardan Labs, Bill Kennedy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Adapted from github.com/ardanlabs/service business/sdk/sqldb/dialect/.

// Package dialect provides cross-cutting helpers that vary between
// SQL engines but are otherwise reusable by every domain store. Today
// Bodh only ships the Postgres dialect; the seam exists so a future
// SQLite-for-tests dialect can be added without touching every store.
//
// Scope intentionally tiny: only add to the Dialect interface when a
// real second engine forces a difference. Adding a method "just in
// case" creates a surface every implementation must satisfy and a
// trap for the dialect-less callers that bypass this package.
package dialect

import "bytes"

// Dialect describes the engine-specific behaviour a store needs in
// order to compose portable SQL from shared fragments.
type Dialect interface {
	// Name reports a short identifier for the engine. Used by
	// observability output — not for branching inside a store
	// (branch on the typed dialect value if you really must).
	Name() string

	// Paginate appends a pagination clause to buf. The clause must
	// consume the named bind variables ":offset" and ":rows_per_page"
	// already supplied by the caller in the parameter map.
	//
	// Bodh stores use OFFSET / FETCH NEXT (SQL:2008 standard) for
	// pagination. The SQLite-for-tests dialect — if and when it
	// arrives — would emit LIMIT/OFFSET instead.
	Paginate(buf *bytes.Buffer)
}
