// Copyright 2024-2026 Pratik Dhanave and Bodh contributors.
// Portions adapted from github.com/ardanlabs/service/business/sdk/unittest,
// Copyright (c) 2018-2026 Ardan Labs, licensed under the Apache License 2.0.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package dbtest

import (
	"context"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// SeedData groups the seed fixtures so a test that calls Seed(t, db) can
// also receive the slices it just wrote. Mirrors the SeedData struct in
// ardanlabs/service/business/sdk/unittest/model.go but shaped to Bodh's
// medical domain (patients, case states, audit events, interactions —
// not users + products).
type SeedData struct {
	TenantID     string
	Patients     []medical.PatientRecord
	CaseStates   []medical.CaseState
	AuditEvents  []audit.Event
	Interactions []eval.InteractionRecord
}

// Table is a table-driven test row. Mirrors the Table struct in
// ardanlabs/service/business/sdk/unittest/model.go.
//
// Compared to a hand-rolled struct, Table standardises three things:
//
//   - Name        — sub-test label, becomes the t.Run argument.
//   - ExpResp     — the expected value; any-typed so tables can hold
//                   different shapes per row (a slice for List tests, a
//                   single record for Get tests).
//   - ExcFunc     — the code under test. Takes the ctx for cancellation,
//                   returns the actual value (any).
//   - CmpFunc     — domain-aware comparison. Returns a non-empty diff
//                   string on failure; empty string on success. Lets each
//                   table choose its comparison: full deep-equal, key
//                   subset, ordering-insensitive, etc.
//
// The "diff" idiom comes straight from ardanlabs's pattern (see
// business/sdk/unittest/unittest.go upstream). It plays nicely with
// go-cmp's cmp.Diff but does NOT introduce a dependency — Bodh keeps
// production deps minimal, and tests can hand-roll the diff with stdlib
// reflect.DeepEqual.
type Table struct {
	Name    string
	ExpResp any
	ExcFunc func(ctx context.Context) any
	CmpFunc func(got any, exp any) string
}
