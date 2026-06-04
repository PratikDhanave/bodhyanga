// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package dbarray

import "testing"

// --- Int64 nil / empty edge cases ---

func TestInt64_Value_Nil(t *testing.T) {
	got, err := Int64(nil).Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("nil Int64 should return nil, got %v", got)
	}
}

func TestInt64_Value_Empty(t *testing.T) {
	got, err := Int64{}.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "{}" {
		t.Errorf("empty Int64 should return %q, got %v", "{}", got)
	}
}

func TestInt64_Value_Multi(t *testing.T) {
	got, err := Int64{10, -20, 300}.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "{10,-20,300}" {
		t.Errorf("got %v, want {10,-20,300}", got)
	}
}

// --- Float64 nil / empty edge cases ---

func TestFloat64_Value_Nil(t *testing.T) {
	got, err := Float64(nil).Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("nil Float64 should return nil, got %v", got)
	}
}

func TestFloat64_Value_Empty(t *testing.T) {
	got, err := Float64{}.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "{}" {
		t.Errorf("empty Float64 should return %q, got %v", "{}", got)
	}
}

func TestFloat64_Value_Multi(t *testing.T) {
	got, err := Float64{3.14, -2.71, 0}.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "{3.14,-2.71,0}"
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// --- Bool nil / empty edge cases ---

func TestBool_Value_Nil(t *testing.T) {
	got, err := Bool(nil).Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("nil Bool should return nil, got %v", got)
	}
}

func TestBool_Value_Empty(t *testing.T) {
	got, err := Bool{}.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "{}" {
		t.Errorf("empty Bool should return %q, got %v", "{}", got)
	}
}

// --- String additional edge cases ---

func TestString_Value_Multi(t *testing.T) {
	got, err := String{"hello", "world", "foo"}.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"hello","world","foo"}`
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}
