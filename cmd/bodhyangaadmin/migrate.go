// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/migrate"
)

// runMigrate is the `bodhyangaadmin migrate` subcommand. Idempotent:
// re-running against a fully migrated DB is a no-op.
//
// Flags
//
//	-dsn       Postgres DSN. Defaults to $BODHYANGA_DB_DSN.
//	-timeout   How long to wait for the whole apply. Defaults to 60s.
//	-dry-run   List pending migrations but don't apply.
//
// Exit codes
//
//	0   schema is up to date (whether or not any migrations were applied).
//	1   open / status check / apply failure. stderr carries the cause.
//	2   bad flags / missing DSN.
func runMigrate(args []string) int {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("BODHYANGA_DB_DSN"), "postgres DSN (or $BODHYANGA_DB_DSN)")
	timeout := fs.Duration("timeout", 60*time.Second, "overall apply timeout")
	dryRun := fs.Bool("dry-run", false, "list pending migrations without applying")
	_ = fs.Parse(args)

	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "bodhyangaadmin migrate: -dsn (or $BODHYANGA_DB_DSN) required")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if *dryRun {
		// Dry-run pretty-prints the file set the runner would apply
		// without touching the DB. Useful in CI to surface a PR that
		// dropped a migration.
		files, err := migrate.Load(migrate.FS())
		if err != nil {
			fmt.Fprintf(os.Stderr, "bodhyangaadmin migrate: load: %v\n", err)
			return 1
		}
		for _, f := range files {
			fmt.Printf("%04d  %s  (%s)\n", f.Version, f.Name, f.Description)
		}
		return 0
	}

	b, err := migrate.PingedBeginner(ctx, *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bodhyangaadmin migrate: open: %v\n", err)
		return 1
	}
	defer b.Close()

	applied, err := migrate.Up(ctx, b, migrate.FS())
	if err != nil {
		fmt.Fprintf(os.Stderr, "bodhyangaadmin migrate: apply: %v\n", err)
		return 1
	}
	if len(applied) == 0 {
		fmt.Println("bodhyangaadmin migrate: schema is up to date")
		return 0
	}
	for _, f := range applied {
		fmt.Printf("applied %04d  %s\n", f.Version, f.Name)
	}
	return 0
}
