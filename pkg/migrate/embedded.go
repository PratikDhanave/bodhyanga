// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package migrate

import (
	"embed"
	"io/fs"
)

// rawFS embeds Bodh's canonical migration set. Files live in
// pkg/migrate/sql/ at NNNN_description.sql.
//
//go:embed sql/*.sql
var rawFS embed.FS

// FS returns the embedded migration set rooted at the SQL directory,
// so callers see a flat fs.FS with NNNN_description.sql at the root.
// Use this for cmd/care + cmd/bodhadmin; tests that want failure
// scenarios pass their own fs.FS into Up directly.
func FS() fs.FS {
	sub, err := fs.Sub(rawFS, "sql")
	if err != nil {
		// Compile-time invariant — the embed directive above
		// guarantees the directory exists in any binary that
		// imports this package. Panic so a CI build catches a
		// future refactor that moves the directory without updating
		// the embed.
		panic("migrate: embed sql subtree unavailable: " + err.Error())
	}
	return sub
}
