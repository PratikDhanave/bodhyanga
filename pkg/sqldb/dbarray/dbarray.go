// Copyright 2025 The Bodhyanga Authors
// Portions Copyright Ardan Labs, Bill Kennedy
// Portions Copyright (c) 2011-2013, 'pq' Contributors
// Portions Copyright (C) 2011 Blake Mizerany
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// The pgx-friendly String / Int64 / Float64 / Bool helpers below are a
// trimmed adaptation of github.com/ardanlabs/service business/sdk/
// sqldb/dbarray, which in turn was taken from github.com/lib/pq under
// the MIT license. The lib/pq notice from the upstream file is
// reproduced here verbatim:
//
//   Code taken from https://github.com/lib/pq
//   Copyright (c) 2011-2013, 'pq' Contributors
//   Portions Copyright (C) 2011 Blake Mizerany
//   Permission is hereby granted, free of charge, to any person
//   obtaining a copy of this software and associated documentation
//   files (the "Software"), to deal in the Software without
//   restriction, including without limitation the rights to use, copy,
//   modify, merge, publish, distribute, sublicense, and/or sell copies
//   of the Software, and to permit persons to whom the Software is
//   furnished to do so, subject to the following conditions:
//   The above copyright notice and this permission notice shall be
//   included in all copies or substantial portions of the Software.
//   THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND.

// Package dbarray exposes the Postgres array driver.Valuer / sql.Scanner
// helpers Bodhyanga stores use to pass []string / []int64 / etc through pgx
// without writing a custom encoder per call site.
//
// Why not use pgx's native array support
//
// pgx already decodes Postgres arrays into []string, []int32, etc when
// the destination slice has the right element type — domain stores
// like pkg/persistence/postgres rely on this. dbarray exists for the
// other half of the trip: passing a Go slice as an array bind parameter
// in a way that survives both pgx and lib/pq (the original ardanlabs
// SDK was sqlx-over-lib/pq).
//
// Today the only caller is pkg/migrate's audit-row insert, which uses
// String for the optional migration-tags list. Future stores that
// accept array bind parameters from the wire can reach for this
// package; the alternative is to inline ANY('{a,b}') text-format
// concatenation per call site, which is exactly what this package
// exists to avoid.
//
// Scope intentionally narrow: scalar single-dimension arrays only. If
// you need multidimensional, switch to pgx's CopyFrom or write the
// encoder inline.
package dbarray

import (
	"database/sql/driver"
	"fmt"
	"strconv"
)

// String represents a one-dimensional array of the PostgreSQL character
// types (text, varchar, ...). The wire format is the standard Postgres
// curly-brace text encoding: {"a","b"} with double-quote escaping for
// embedded quotes and backslashes.
type String []string

// Value implements driver.Valuer.
//
// The output is parseable by every Postgres driver Bodhyanga supports
// (pgx/v5 and lib/pq); we choose the text format rather than binary
// because pgx accepts text for parameter binding to TEXT[] columns
// without a typeOID hint.
func (a String) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	if len(a) == 0 {
		return "{}", nil
	}
	b := []byte{'{'}
	for i, s := range a {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendQuoted(b, s)
	}
	b = append(b, '}')
	return string(b), nil
}

// Int64 represents a one-dimensional array of PostgreSQL integer types.
type Int64 []int64

// Value implements driver.Valuer.
func (a Int64) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	if len(a) == 0 {
		return "{}", nil
	}
	b := []byte{'{'}
	for i, n := range a {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendInt(b, n, 10)
	}
	b = append(b, '}')
	return string(b), nil
}

// Float64 represents a one-dimensional array of PostgreSQL double-precision.
type Float64 []float64

// Value implements driver.Valuer.
func (a Float64) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	if len(a) == 0 {
		return "{}", nil
	}
	b := []byte{'{'}
	for i, n := range a {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendFloat(b, n, 'f', -1, 64)
	}
	b = append(b, '}')
	return string(b), nil
}

// Bool represents a one-dimensional array of PostgreSQL boolean.
type Bool []bool

// Value implements driver.Valuer.
func (a Bool) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	if len(a) == 0 {
		return "{}", nil
	}
	b := []byte{'{'}
	for i, v := range a {
		if i > 0 {
			b = append(b, ',')
		}
		if v {
			b = append(b, 't')
		} else {
			b = append(b, 'f')
		}
	}
	b = append(b, '}')
	return string(b), nil
}

// appendQuoted writes s into b as a Postgres-array-quoted element.
// The escaping rules are documented at
// https://www.postgresql.org/docs/current/arrays.html#ARRAYS-IO: any
// embedded " or \ is preceded by a backslash; nothing else needs
// special handling for a one-dimensional text array.
func appendQuoted(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' {
			b = append(b, '\\')
		}
		b = append(b, c)
	}
	return append(b, '"')
}

// _ unused import guard to keep gofmt happy if the package is later
// extended with reflection-based variants (the upstream Generic type).
var _ = fmt.Sprintf
