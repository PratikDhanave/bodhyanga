// throughput_test.go — named benchmark entry points that the bench-suite
// command discovers by name. Each function is a thin wrapper over the
// implementations in bench_test.go so callers get stable symbol names.
//
// Run all:
//
//	go test ./pkg/bench/... -bench=. -benchtime=5s
package bench

import "testing"

// BenchmarkBusThroughput is the entry point benchmark for InMemoryBus
// publish-to-delivery throughput.
//
// Sub-benchmarks (N=1, 5, 10 subscribers) exercise:
//   - Baseline single-subscriber throughput
//   - Fan-out overhead as subscriber count grows
//   - p50/p95/p99 delivery latency (via BenchmarkBusThroughputLatency)
func BenchmarkBusThroughputEntry(b *testing.B) {
	BenchmarkBusThroughput(b)
}

// BenchmarkOrchestratorThroughputEntry is the entry point for the 3-hop
// pipeline benchmark (intake → diagnostician → reporter).
func BenchmarkOrchestratorThroughputEntry(b *testing.B) {
	BenchmarkOrchestratorThroughput(b)
}

// BenchmarkLLMClientMockEntry is the entry point for the TracingClient
// overhead measurement.
func BenchmarkLLMClientMockEntry(b *testing.B) {
	BenchmarkLLMClientMock(b)
}

// BenchmarkRegistryLookupEntry is the entry point for capability-lookup
// scaling (N=10, 100, 1000 agents).
func BenchmarkRegistryLookupEntry(b *testing.B) {
	BenchmarkRegistryLookup(b)
}

// BenchmarkGovernanceEvalEntry is the entry point for CompositePolicy
// evaluation with 5 chained sub-policies.
func BenchmarkGovernanceEvalEntry(b *testing.B) {
	BenchmarkGovernanceEval(b)
}
