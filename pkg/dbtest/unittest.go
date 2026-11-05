// Copyright 2024-2026 Pratik Dhanave and Bodhyanga contributors.
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
	"testing"
)

// TableOption tweaks a single Run invocation. Modelled after ardanlabs's
// OptionFunc — kept minimal because Bodhyanga tests rarely need more than skip.
type TableOption func(*tableOptions)

type tableOptions struct {
	skip    bool
	skipMsg string
}

// WithSkip marks the whole table run as Skipped with msg. Useful for
// integration tables guarded by an env var (e.g. Docker availability) so
// the table still appears in the test list but doesn't run.
func WithSkip(skip bool, msg string) TableOption {
	return func(o *tableOptions) {
		o.skip = skip
		o.skipMsg = msg
	}
}

// Run executes every row in table as a sub-test named "<testName>/<row.Name>".
//
// Adapted from ardanlabs/service/business/sdk/unittest/unittest.go.Run: the
// shape is identical, but Bodhyanga's variant defaults to context.Background()
// for the per-row call (the upstream uses the same default) and prints the
// diff via t.Logf rather than fmt.Printf so it appears in test output
// captured by -v.
//
// Each row calls ExcFunc(ctx) → CmpFunc(got, expected). A non-empty diff
// string fails the sub-test; empty diff passes. This trades a strict
// reflect.DeepEqual for caller-controlled comparison, which is the right
// trade for clinical fixtures where "same patient" might mean "same ID +
// same conditions" rather than "every byte identical."
func Run(t *testing.T, table []Table, testName string, options ...TableOption) {
	t.Helper()

	opts := &tableOptions{}
	for _, f := range options {
		f(opts)
	}
	if opts.skip {
		t.Skipf("%s: %s", testName, opts.skipMsg)
	}

	for _, tt := range table {
		tt := tt
		t.Run(testName+"/"+tt.Name, func(t *testing.T) {
			gotResp := tt.ExcFunc(context.Background())
			diff := tt.CmpFunc(gotResp, tt.ExpResp)
			if diff != "" {
				t.Log("DIFF")
				t.Logf("%s", diff)
				t.Log("GOT")
				t.Logf("%#v", gotResp)
				t.Log("EXP")
				t.Logf("%#v", tt.ExpResp)
				t.Fatalf("dbtest: %s/%s: expected response not received", testName, tt.Name)
			}
		})
	}
}
