// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Command bodhyangaadmin is the operator CLI for the Bodhyanga control plane.
// Subcommands run discrete jobs that would otherwise be one-off psql
// scripts or in-process API calls.
//
// Subcommands (one per file in this directory):
//
//	migrate    Apply pending schema migrations (idempotent).
//	keys       Generate and list RSA 2048 JWT signing keypairs.
//	token      Mint a signed JWT for dev/testing (NOT for production).
//
// Usage:
//
//	bodhyangaadmin migrate [-dsn=...]
//	bodhyangaadmin keys generate [-out <dir>] [-kid <name>]
//	bodhyangaadmin keys list [-dir <dir>]
//	bodhyangaadmin token issue -kid <name> -dir <dir> -role <role> [...]
//
// Operators run it from CI / a bastion / a sidecar; cmd/care also
// runs migrations on boot as a defence in depth.
package main

import (
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
	case "keys":
		runKeys(args)
	case "token":
		runToken(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "bodhyangaadmin: unknown subcommand %q\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `bodhyangaadmin -- Bodhyanga operator CLI

usage:
  bodhyangaadmin <subcommand> [flags]

subcommands:
  migrate    Apply pending schema migrations (idempotent).
  keys       Generate and list RSA 2048 JWT signing keypairs.
  token      Mint a signed JWT for dev / testing.

environment:
  BODHYANGA_DB_DSN          Postgres DSN (overridden by migrate -dsn).
  BODHYANGA_AUTH_KEYS_DIR   Directory of <kid>.pem files (overridden by -dir).

`)
}
