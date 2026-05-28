// Package main for cmd/bodhadmin is the operator CLI subcommand
// dispatcher. Each subcommand lives in its own file in this directory
// (migrate.go, etc.) so a future PR adding `bodhadmin seed`,
// `bodhadmin audit-export`, etc. only adds a file + a switch arm in
// main.go.
//
// Doc files follow the rest of the codebase convention: every .go
// source file has a sibling _doc.go (see AGENTS.md § Coding
// conventions). This file documents the dispatcher; the subcommand
// implementations document themselves inline.
//
// This package is sibling to cmd/bodhctl which is the operator CLI
// for the running care API (HTTP-talking client). bodhadmin is the
// out-of-band CLI for jobs the running API cannot do itself
// (schema migrations, role rotations, audit exports).
package main
