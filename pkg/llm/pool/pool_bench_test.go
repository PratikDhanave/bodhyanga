package pool

import (
	"context"
	"testing"
)

// BenchmarkPool_GetWarm measures the steady-state cost of a Get on a
// resident, refcount==0 entry — the common case during agent execution.
func BenchmarkPool_GetWarm(b *testing.B) {
	ff := newFakeFactory()
	p := NewPool(ff.make, Options{})
	defer p.Close()

	spec := ModelSpec{Provider: "p", Model: "warm"}
	ctx := context.Background()
	if _, err := p.Get(ctx, spec); err != nil {
		b.Fatalf("warmup Get: %v", err)
	}
	p.Release(spec)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := p.Get(ctx, spec)
		if err != nil {
			b.Fatalf("Get: %v", err)
		}
		p.Release(spec)
	}
}

// BenchmarkPool_GetCold measures the cost of a Get that always misses —
// it tears down the entry between iterations so each call goes through
// the factory path. Useful as a proxy for "what does the construction
// overhead cost on top of the factory itself?"
func BenchmarkPool_GetCold(b *testing.B) {
	ff := newFakeFactory()
	p := NewPool(ff.make, Options{})
	defer p.Close()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// New spec each iteration so the pool always misses.
		spec := ModelSpec{Provider: "p", Model: "cold", Tag: bToString(i)}
		_, err := p.Get(ctx, spec)
		if err != nil {
			b.Fatalf("Get: %v", err)
		}
		p.Release(spec)
	}
}

// bToString avoids pulling strconv into the bench file; it's a tiny
// integer-to-decimal converter only used to fan out unique spec keys.
func bToString(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
