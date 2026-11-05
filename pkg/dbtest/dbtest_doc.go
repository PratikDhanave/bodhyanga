// Copyright 2024-2026 Pratik Dhanave and Bodhyanga contributors.
// Portions adapted from github.com/ardanlabs/service/business/sdk/dbtest
// and business/sdk/unittest, Copyright (c) 2018-2026 Ardan Labs, licensed
// under the Apache License 2.0.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Package dbtest is Bodhyanga's shared Postgres integration-test harness.
//
// # Origin and credit
//
// The container-once-per-binary + table-driven sub-test pattern in this
// package is adapted from Bill Kennedy's ardanlabs/service template, in
// particular:
//
//   - business/sdk/dbtest/dbtest.go       (Database type, container spin-up,
//                                          per-test scope, t.Cleanup teardown)
//   - business/sdk/dbtest/business.go     (BusDomain grouping of test fixtures)
//   - business/sdk/unittest/unittest.go   (Run / Table / WithSkip)
//   - business/sdk/unittest/model.go      (SeedData)
//
// The originals are Copyright (c) 2018-2026 Ardan Labs and licensed under
// the Apache License 2.0. The full attribution is in the repository's root
// NOTICE file; each source file in pkg/dbtest carries its own Apache 2.0
// header naming the upstream.
//
// # What Bodhyanga changed
//
// Bodhyanga's adaptation departs from the upstream in three load-bearing ways:
//
//  1. Tenant scoping by RLS, not per-test database. ardanlabs/service
//     creates a fresh "<random>.<image>" database per test and runs the full
//     migration on each. Bodhyanga has Postgres row-level security (db/migrations/
//     0002_rls.sql) and a bodhyanga_app role that NOBYPASSRLS — tearing it down
//     per test would blow the 20% test-runtime budget. Instead NewDB(t)
//     returns a *DB whose TenantID is a unique "tenant_test_<hex>" scope.
//     RLS makes other tenants' rows invisible.
//
//  2. PHI-safe seed by construction. The synthetic-only invariant for
//     pkg/dbtest/testdata/ and pkg/dbtest/seed.go is enforced by
//     TestFixturesAreSynthetic in dbtest_test.go: no realistic names, no
//     MRN-shaped digit strings, no SSN-shaped strings. The "patient_test_NNN"
//     / "case_test_NNN" / "event_test_NNN" prefixes are mandatory.
//
//  3. Append-only audit semantics preserved during cleanup. db.Wipe deletes
//     patient_records under RLS but does NOT delete audit_events: the
//     application role has GRANT SELECT, INSERT only on that table by design
//     (HIPAA §164.312(b)), and the test harness honours that boundary.
//     Audit rows from prior tenants are invisible to new tenants via RLS,
//     and the container goes away at process exit.
//
// # Usage
//
// A minimal integration test:
//
//	func TestSomething(t *testing.T) {
//	    db := dbtest.NewDB(t)               // shared container, fresh tenant
//	    dbtest.SeedPatientsOnly(t, db)      // synthetic roster
//	    patients, err := db.Store.ListPatients(t.Context(), db.TenantID)
//	    if err != nil { t.Fatal(err) }
//	    if len(patients) != 3 { t.Fatalf("got %d, want 3", len(patients)) }
//	}
//
// A table-driven test using the unittest helpers:
//
//	func TestListByCondition(t *testing.T) {
//	    db := dbtest.NewDB(t)
//	    dbtest.SeedPatientsOnly(t, db)
//	    dbtest.Run(t, []dbtest.Table{
//	        {
//	            Name: "diabetes",
//	            ExpResp: 1,
//	            ExcFunc: func(ctx context.Context) any {
//	                xs, _ := db.Store.FilterPatientsByCondition(ctx, db.TenantID, "E11.9")
//	                return len(xs)
//	            },
//	            CmpFunc: func(got, exp any) string {
//	                if got == exp { return "" }
//	                return fmt.Sprintf("got=%v exp=%v", got, exp)
//	            },
//	        },
//	    }, "TestListByCondition")
//	}
//
// # When Track C lands
//
// Track C will move the canonical migrations from db/migrations/ to
// pkg/migrate/sql/. When that happens, StartContainer should be retargeted
// to read the migrations from pkg/migrate.Embed() (or equivalent) so the
// harness stays in lock-step with the production migrator. Until then,
// dbtest piggy-backs on the pgx-embedded copy in pkg/persistence/postgres/
// migrations/ via postgres.New().
//
// # Performance budget
//
// One Postgres container per test binary, ~5-7s warm-up cost. NewDB(t)
// itself is O(1) — no per-test DDL, no per-test schema clone. The 20%
// runtime budget against the pre-existing per-test container model is
// trivially met (the upstream pattern would have been slower, not faster).
package dbtest
