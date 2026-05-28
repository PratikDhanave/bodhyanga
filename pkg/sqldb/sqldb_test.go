// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package sqldb

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestClassifyError_UniqueViolation maps SQLSTATE 23505 onto the
// typed sentinel. Domain stores branch on errors.Is so the mapping
// must round-trip correctly.
func TestClassifyError_UniqueViolation(t *testing.T) {
	raw := &pgconn.PgError{Code: uniqueViolation, Message: "dup"}
	classified := ClassifyError(raw)
	if !errors.Is(classified, ErrDuplicatedEntry) {
		t.Fatalf("classified=%v not a ErrDuplicatedEntry", classified)
	}
}

// TestClassifyError_UndefinedTable maps SQLSTATE 42P01.
func TestClassifyError_UndefinedTable(t *testing.T) {
	raw := &pgconn.PgError{Code: undefinedTable, Message: "no such table"}
	classified := ClassifyError(raw)
	if !errors.Is(classified, ErrUndefinedTable) {
		t.Fatalf("classified=%v not a ErrUndefinedTable", classified)
	}
}

// TestClassifyError_Passthrough leaves unrecognised errors alone so
// callers can branch on the underlying cause without losing context.
func TestClassifyError_Passthrough(t *testing.T) {
	src := errors.New("some other failure")
	if got := ClassifyError(src); got != src {
		t.Fatalf("got %v, want passthrough of original", got)
	}
	if got := ClassifyError(nil); got != nil {
		t.Fatalf("nil in should yield nil out; got %v", got)
	}
}

// TestBuildDSN_HappyPath verifies the synthesised DSN matches the
// upstream's shape (postgres://user:pw@host/db?sslmode=...&timezone=utc).
// Operators sometimes paste the synthesised DSN; an unexpected change
// here breaks their muscle memory.
func TestBuildDSN_HappyPath(t *testing.T) {
	got := buildDSN(Config{
		User: "bodh_app", Password: "pw",
		Host: "db:5432", Name: "bodh",
		DisableTLS: true,
	})
	want := "postgres://bodh_app:pw@db:5432/bodh?sslmode=disable&timezone=utc"
	if got != want {
		t.Fatalf("buildDSN got %q want %q", got, want)
	}
}

// TestBuildDSN_IncompleteReturnsEmpty so Open can detect "no config
// supplied" and surface a helpful error.
func TestBuildDSN_IncompleteReturnsEmpty(t *testing.T) {
	if got := buildDSN(Config{}); got != "" {
		t.Fatalf("empty config should yield empty DSN; got %q", got)
	}
}

// TestNewBeginner_NilPool returns nil so cmd/care can defer Close on
// the result without an extra nil-check.
func TestNewBeginner_NilPool(t *testing.T) {
	if got := NewBeginner(nil); got != nil {
		t.Fatalf("NewBeginner(nil) = %v, want nil", got)
	}
}
