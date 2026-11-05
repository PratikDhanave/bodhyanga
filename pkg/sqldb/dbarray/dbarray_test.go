// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package dbarray

import "testing"

// TestString_Value covers the happy path + edge cases (nil, empty,
// embedded quote) so future maintainers can regression-check the
// Postgres text-format escaping rules.
func TestString_Value(t *testing.T) {
	cases := []struct {
		name string
		in   String
		want any
	}{
		{"nil", nil, nil},
		{"empty", String{}, "{}"},
		{"single", String{"a"}, `{"a"}`},
		{"multi", String{"a", "b"}, `{"a","b"}`},
		{"quote-and-backslash", String{`a"b\c`}, `{"a\"b\\c"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.Value()
			if err != nil {
				t.Fatalf("Value: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestInt64_Value(t *testing.T) {
	got, _ := Int64{1, 2, 3}.Value()
	if got != "{1,2,3}" {
		t.Fatalf("got %v", got)
	}
}

func TestFloat64_Value(t *testing.T) {
	got, _ := Float64{1.5, 2.25}.Value()
	if got != "{1.5,2.25}" {
		t.Fatalf("got %v", got)
	}
}

func TestBool_Value(t *testing.T) {
	got, _ := Bool{true, false, true}.Value()
	if got != "{t,f,t}" {
		t.Fatalf("got %v", got)
	}
}

// Cover nil and empty cases for Int64.
func TestInt64_Value_NilAndEmpty(t *testing.T) {
	// nil returns nil
	got, err := Int64(nil).Value()
	if err != nil {
		t.Fatalf("nil Int64 error: %v", err)
	}
	if got != nil {
		t.Fatalf("nil Int64 got %v, want nil", got)
	}

	// empty returns "{}"
	got, err = Int64{}.Value()
	if err != nil {
		t.Fatalf("empty Int64 error: %v", err)
	}
	if got != "{}" {
		t.Fatalf("empty Int64 got %v, want {}", got)
	}
}

// Cover nil and empty cases for Float64.
func TestFloat64_Value_NilAndEmpty(t *testing.T) {
	got, err := Float64(nil).Value()
	if err != nil {
		t.Fatalf("nil Float64 error: %v", err)
	}
	if got != nil {
		t.Fatalf("nil Float64 got %v, want nil", got)
	}

	got, err = Float64{}.Value()
	if err != nil {
		t.Fatalf("empty Float64 error: %v", err)
	}
	if got != "{}" {
		t.Fatalf("empty Float64 got %v, want {}", got)
	}
}

// Cover nil and empty cases for Bool.
func TestBool_Value_NilAndEmpty(t *testing.T) {
	got, err := Bool(nil).Value()
	if err != nil {
		t.Fatalf("nil Bool error: %v", err)
	}
	if got != nil {
		t.Fatalf("nil Bool got %v, want nil", got)
	}

	got, err = Bool{}.Value()
	if err != nil {
		t.Fatalf("empty Bool error: %v", err)
	}
	if got != "{}" {
		t.Fatalf("empty Bool got %v, want {}", got)
	}
}

// Cover single-element cases for Int64, Float64, Bool.
func TestInt64_Value_Single(t *testing.T) {
	got, _ := Int64{42}.Value()
	if got != "{42}" {
		t.Fatalf("got %v, want {42}", got)
	}
}

func TestFloat64_Value_Single(t *testing.T) {
	got, _ := Float64{3.14}.Value()
	if got != "{3.14}" {
		t.Fatalf("got %v, want {3.14}", got)
	}
}

func TestBool_Value_Single(t *testing.T) {
	got, _ := Bool{false}.Value()
	if got != "{f}" {
		t.Fatalf("got %v, want {f}", got)
	}
}
