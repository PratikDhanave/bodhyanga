// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Command bodhadmin is the operator CLI for the Bodh control plane.
// It is intentionally minimal: subcommands run discrete jobs that
// would otherwise be one-off psql scripts or in-process API calls.
//
// Subcommands (one per file in this directory):
//
//	migrate    Apply pending schema migrations.
//
// Usage:
//
//	bodhadmin migrate [-dsn=...]
//
// The binary is stdlib-only at the cmd/ layer (it imports pkg/migrate
// + pkg/sqldb, both of which already depend on pgx/v5 — no new deps).
// Operators run it from CI / a bastion / a sidecar; cmd/care also
// runs migrations on boot as a defence in depth.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "migrate":
		os.Exit(runMigrate(args))
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "bodhadmin: unknown subcommand %q\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `bodhadmin -- Bodh operator CLI

usage:
  bodhadmin <subcommand> [flags]

subcommands:
  migrate    Apply pending schema migrations (idempotent).

environment:
  BODH_DB_DSN   Postgres DSN (overridden by -dsn).

`)
	// Per-subcommand flag help is reachable via "bodhadmin migrate -h".
	_ = flag.CommandLine
}
