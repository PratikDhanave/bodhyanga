// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package multiturn

import (
	"testing"
)

// -- ToolOrderCorrect ---------------------------------------------------------

func TestToolOrderCorrect_NoConstraint(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile", "writeFile"}}
	target := &MultiTurnTarget{} // ExpectedToolOrder empty → always 1.0
	if got := ToolOrderCorrect(result, target); got != 1.0 {
		t.Errorf("want 1.0, got %v", got)
	}
}

func TestToolOrderCorrect_Exact(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile", "writeFile"}}
	target := &MultiTurnTarget{ExpectedToolOrder: []string{"readFile", "writeFile"}}
	if got := ToolOrderCorrect(result, target); got != 1.0 {
		t.Errorf("want 1.0, got %v", got)
	}
}

func TestToolOrderCorrect_ExtraCallsAllowed(t *testing.T) {
	// Agent called an extra tool after the required sequence — still passes.
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile", "writeFile", "listDir"}}
	target := &MultiTurnTarget{ExpectedToolOrder: []string{"readFile", "writeFile"}}
	if got := ToolOrderCorrect(result, target); got != 1.0 {
		t.Errorf("want 1.0 for extra calls after expected sequence, got %v", got)
	}
}

func TestToolOrderCorrect_WrongOrder(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"writeFile", "readFile"}}
	target := &MultiTurnTarget{ExpectedToolOrder: []string{"readFile", "writeFile"}}
	if got := ToolOrderCorrect(result, target); got != 0.0 {
		t.Errorf("want 0.0 for wrong order, got %v", got)
	}
}

func TestToolOrderCorrect_TooFewCalls(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile"}}
	target := &MultiTurnTarget{ExpectedToolOrder: []string{"readFile", "writeFile"}}
	if got := ToolOrderCorrect(result, target); got != 0.0 {
		t.Errorf("want 0.0 when agent made fewer calls than expected, got %v", got)
	}
}

func TestToolOrderCorrect_EmptyResult(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{}}
	target := &MultiTurnTarget{ExpectedToolOrder: []string{"readFile"}}
	if got := ToolOrderCorrect(result, target); got != 0.0 {
		t.Errorf("want 0.0 when no calls made, got %v", got)
	}
}

// -- ToolsAvoided -------------------------------------------------------------

func TestToolsAvoided_NoConstraint(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile"}}
	target := &MultiTurnTarget{} // ForbiddenTools empty → always 1.0
	if got := ToolsAvoided(result, target); got != 1.0 {
		t.Errorf("want 1.0, got %v", got)
	}
}

func TestToolsAvoided_NoForbiddenCalled(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile"}}
	target := &MultiTurnTarget{ForbiddenTools: []string{"deleteFile", "runCommand"}}
	if got := ToolsAvoided(result, target); got != 1.0 {
		t.Errorf("want 1.0 when forbidden tools not called, got %v", got)
	}
}

func TestToolsAvoided_ForbiddenCalled(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile", "deleteFile"}}
	target := &MultiTurnTarget{ForbiddenTools: []string{"deleteFile"}}
	if got := ToolsAvoided(result, target); got != 0.0 {
		t.Errorf("want 0.0 when forbidden tool was called, got %v", got)
	}
}

func TestToolsAvoided_NoCallsMadeAtAll(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{}}
	target := &MultiTurnTarget{ForbiddenTools: []string{"readFile", "runCommand"}}
	if got := ToolsAvoided(result, target); got != 1.0 {
		t.Errorf("want 1.0 when no calls were made, got %v", got)
	}
}

func TestToolsAvoided_MultipleCallsOneForbidden(t *testing.T) {
	result := &MultiTurnResult{ToolCallOrder: []string{"readFile", "listDir", "runCommand"}}
	target := &MultiTurnTarget{ForbiddenTools: []string{"runCommand"}}
	if got := ToolsAvoided(result, target); got != 0.0 {
		t.Errorf("want 0.0 when any forbidden tool is called, got %v", got)
	}
}

// -- Combined evaluator pass/fail scenarios ----------------------------------

func TestCombined_FreshTaskSuccess(t *testing.T) {
	// Agent read the right file → order correct, no forbidden tools used.
	result := &MultiTurnResult{
		Text:          `The API endpoint is "https://api.example.com/v1".`,
		ToolCallOrder: []string{"readFile"},
	}
	target := &MultiTurnTarget{
		ExpectedToolOrder: []string{"readFile"},
		ForbiddenTools:    []string{"writeFile", "deleteFile"},
		OriginalTask:      "Read config.json and report the API endpoint",
		MockToolResults:   map[string]string{"readFile": `{"apiEndpoint":"https://api.example.com/v1"}`},
	}
	if got := ToolOrderCorrect(result, target); got != 1.0 {
		t.Errorf("ToolOrder want 1.0, got %v", got)
	}
	if got := ToolsAvoided(result, target); got != 1.0 {
		t.Errorf("ToolsAvoided want 1.0, got %v", got)
	}
}

func TestCombined_NegativeSimpleQuestion(t *testing.T) {
	// Agent correctly answered a math question without using any tools.
	result := &MultiTurnResult{
		Text:          "2 + 2 = 4",
		ToolCallOrder: []string{},
	}
	target := &MultiTurnTarget{
		ForbiddenTools: []string{"readFile", "runCommand", "writeFile"},
		OriginalTask:   "What is 2 + 2?",
	}
	if got := ToolsAvoided(result, target); got != 1.0 {
		t.Errorf("ToolsAvoided want 1.0, got %v", got)
	}
}
