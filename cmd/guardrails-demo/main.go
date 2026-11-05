// Command guardrails-demo demonstrates the guardrail pipeline with sample
// inputs that trigger various checks. It exercises every built-in guardrail
// and prints the result and recommended action for each.
//
// Usage:
//
//	go run ./cmd/guardrails-demo
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/PratikDhanave/bodhyanga/pkg/guardrails"
)

func main() {
	fmt.Println("=== Bodhyanga Guardrails Pipeline Demo ===")
	fmt.Println()

	// Build the default pipeline with all built-in guardrails.
	pipeline := guardrails.DefaultPipeline()

	// Sample outputs to test against the pipeline.
	samples := []struct {
		label  string
		output string
	}{
		{
			label:  "Safe clinical output",
			output: "The patient presents with acute bronchitis. Recommend rest and hydration. This is AI-generated and not for clinical use.",
		},
		{
			label:  "PII leak (SSN)",
			output: "Patient John Doe, SSN 123-45-6789, was admitted on Monday.",
		},
		{
			label:  "PII leak (email)",
			output: "Contact the patient at jane.doe@hospital.org for follow-up.",
		},
		{
			label:  "SQL injection",
			output: "SELECT * FROM patients; DROP TABLE patients;",
		},
		{
			label:  "SQL injection (UNION SELECT)",
			output: "SELECT name FROM patients UNION SELECT password FROM admin_users",
		},
		{
			label:  "Invalid SQL syntax",
			output: "GIBBERISH QUERY THING",
		},
		{
			label:  "Valid SQL",
			output: "SELECT patient_id, diagnosis FROM encounters WHERE age > 65",
		},
		{
			label:  "Toxic content (harmful advice)",
			output: "You should stop taking your medication and try this miracle cure instead.",
		},
		{
			label:  "Toxic content (anti-vax)",
			output: "Studies show that vaccines cause autism, so avoid them.",
		},
		{
			label:  "Excessively long output",
			output: strings.Repeat("This is a very long output that goes on and on. ", 100),
		},
		{
			label:  "Valid JSON",
			output: `{"diagnosis": "influenza", "confidence": 0.92, "icd10": "J09"}`,
		},
		{
			label:  "Invalid JSON",
			output: `{"diagnosis": "influenza", confidence: 0.92}`,
		},
		{
			label:  "Diagnosis without disclaimer",
			output: "Differential diagnosis includes acute myocardial infarction and pulmonary embolism. Recommend troponin and D-dimer.",
		},
		{
			label:  "Diagnosis with disclaimer",
			output: "Differential diagnosis includes acute myocardial infarction. This is not for clinical use and should be reviewed by a physician.",
		},
	}

	ctx := context.Background()
	for _, s := range samples {
		result, action := pipeline.Run(ctx, s.output)
		status := "PASS"
		if !result.Safe {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s\n", status, s.label)
		if !result.Safe {
			fmt.Printf("  Rule:    %s\n", result.Rule)
			fmt.Printf("  Reason:  %s\n", result.Reason)
			fmt.Printf("  Action:  %s\n", action)
		}
		fmt.Printf("  Latency: %s\n", result.Latency)
		fmt.Println()
	}

	// Print aggregate statistics.
	stats := pipeline.Stats()
	fmt.Println("=== Pipeline Statistics ===")
	fmt.Printf("Total checks:  %d\n", stats.TotalChecks)
	fmt.Printf("Total blocked: %d\n", stats.TotalBlocked)
	fmt.Printf("Avg latency:   %s\n", stats.AvgLatency)
	fmt.Println("Blocks by rule:")
	for rule, count := range stats.ByRule {
		fmt.Printf("  %-30s %d\n", rule, count)
	}
}
