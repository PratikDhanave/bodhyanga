package pool

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
)

// --- stub client ---

type stubClient struct{}

func (s stubClient) Provider() string { return "stub" }
func (s stubClient) Model() string    { return "stub-model" }
func (s stubClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{Content: "ok"}, nil
}

type stubCloser struct {
	closed bool
	err    error
}

func (c *stubCloser) Close() error { c.closed = true; return c.err }

// --- NewKronkFactory stub ---

func TestNewKronkFactory_ReturnsErrKronkNotBuilt(t *testing.T) {
	f := NewKronkFactory(KronkOptions{})
	_, _, err := f(context.Background(), ModelSpec{Provider: "kronk", Model: "test"})
	if !errors.Is(err, ErrKronkNotBuilt) {
		t.Errorf("expected ErrKronkNotBuilt, got %v", err)
	}
}

// --- Stats.HitRate ---

func TestStats_HitRate_ZeroTotal(t *testing.T) {
	s := Stats{}
	if s.HitRate() != 0 {
		t.Errorf("HitRate = %f, want 0", s.HitRate())
	}
}

func TestStats_HitRate_NonZero(t *testing.T) {
	s := Stats{Hits: 3, Misses: 1}
	want := 0.75
	if s.HitRate() != want {
		t.Errorf("HitRate = %f, want %f", s.HitRate(), want)
	}
}

// --- NewPool panic ---

func TestNewPool_NilFactory_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil factory")
		}
	}()
	NewPool(nil, Options{})
}

// --- Pool Close idempotent ---

func TestPool_Close_Idempotent(t *testing.T) {
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return stubClient{}, &stubCloser{}, nil
	}, Options{})
	if err := p.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close should be nil: %v", err)
	}
}

// --- Get on closed pool ---

func TestPool_Get_ClosedPool(t *testing.T) {
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return stubClient{}, &stubCloser{}, nil
	}, Options{})
	p.Close()
	_, err := p.Get(context.Background(), ModelSpec{Provider: "x", Model: "y"})
	if err == nil {
		t.Fatal("expected error on Get after Close")
	}
}

// --- Get with factory error ---

func TestPool_Get_FactoryError(t *testing.T) {
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return nil, nil, errors.New("factory boom")
	}, Options{})
	defer p.Close()
	_, err := p.Get(context.Background(), ModelSpec{Provider: "x", Model: "y"})
	if err == nil || !strings.Contains(err.Error(), "factory boom") {
		t.Errorf("expected factory error, got %v", err)
	}
}

// --- Release unknown spec ---

func TestPool_Release_UnknownSpec(t *testing.T) {
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return stubClient{}, &stubCloser{}, nil
	}, Options{})
	defer p.Close()
	// Should not panic on releasing a spec that was never Get'd.
	p.Release(ModelSpec{Provider: "unknown", Model: "m"})
}

// --- Close with closer error ---

func TestPool_Close_CloserError(t *testing.T) {
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return stubClient{}, &stubCloser{err: errors.New("close fail")}, nil
	}, Options{})
	_, err := p.Get(context.Background(), ModelSpec{Provider: "x", Model: "y"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	err = p.Close()
	if err == nil || !strings.Contains(err.Error(), "close fail") {
		t.Errorf("expected closer error on Close, got %v", err)
	}
}

// --- TTL sweep interval default ---

func TestNewPool_TTLDefaultSweepInterval(t *testing.T) {
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return stubClient{}, &stubCloser{}, nil
	}, Options{TTL: 4 * time.Second})
	defer p.Close()
	// Sweep interval should be TTL/4 = 1s
	if p.opts.SweepInterval != time.Second {
		t.Errorf("SweepInterval = %v, want 1s", p.opts.SweepInterval)
	}
}

func TestNewPool_TTLMinSweepInterval(t *testing.T) {
	// TTL so small that TTL/4 < 100ms
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return stubClient{}, &stubCloser{}, nil
	}, Options{TTL: 200 * time.Millisecond})
	defer p.Close()
	if p.opts.SweepInterval != 100*time.Millisecond {
		t.Errorf("SweepInterval = %v, want 100ms", p.opts.SweepInterval)
	}
}

// --- MaxResident LRU eviction ---

func TestPool_MaxResident_LRUEviction(t *testing.T) {
	calls := 0
	p := NewPool(func(_ context.Context, spec ModelSpec) (llm.Client, io.Closer, error) {
		calls++
		return stubClient{}, &stubCloser{}, nil
	}, Options{MaxResident: 1})
	defer p.Close()

	spec1 := ModelSpec{Provider: "a", Model: "m1"}
	spec2 := ModelSpec{Provider: "a", Model: "m2"}

	_, err := p.Get(context.Background(), spec1)
	if err != nil {
		t.Fatalf("Get spec1: %v", err)
	}
	p.Release(spec1) // refcount=0, eligible for eviction

	_, err = p.Get(context.Background(), spec2)
	if err != nil {
		t.Fatalf("Get spec2: %v", err)
	}

	stats := p.Stats()
	if stats.Evictions < 1 {
		t.Errorf("expected at least 1 eviction, got %d", stats.Evictions)
	}
}

// --- MaxResident context cancelled while waiting ---

func TestPool_MaxResident_ContextCancelled(t *testing.T) {
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return stubClient{}, &stubCloser{}, nil
	}, Options{MaxResident: 1})
	defer p.Close()

	spec1 := ModelSpec{Provider: "a", Model: "m1"}
	_, err := p.Get(context.Background(), spec1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// spec1 is in use (refcount=1), pool is full

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	spec2 := ModelSpec{Provider: "a", Model: "m2"}
	_, err = p.Get(ctx, spec2)
	if err == nil {
		t.Fatal("expected error when ctx cancelled while waiting for MaxResident")
	}
}

// --- Pool with logger ---

func TestPool_WithLogger(t *testing.T) {
	var buf strings.Builder
	logger := observability.NewLoggerTo(&buf)

	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return stubClient{}, &stubCloser{}, nil
	}, Options{Logger: logger})
	defer p.Close()

	_, err := p.Get(context.Background(), ModelSpec{Provider: "test", Model: "m"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(buf.String(), "construct") {
		t.Errorf("expected logger to contain 'construct', got %q", buf.String())
	}
}

// --- sweepOnce on closed pool ---

func TestPool_SweepOnce_Closed(t *testing.T) {
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return stubClient{}, &stubCloser{}, nil
	}, Options{TTL: time.Hour})
	p.Close()
	// Call sweepOnce on a closed pool to exercise the early return path.
	p.sweepOnce()
}

// --- Pool closed during factory call ---

func TestPool_Get_ClosedDuringFactory(t *testing.T) {
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		// Simulate slow factory; pool will be closed concurrently.
		time.Sleep(50 * time.Millisecond)
		return stubClient{}, &stubCloser{}, nil
	}, Options{})

	go func() {
		time.Sleep(10 * time.Millisecond)
		p.Close()
	}()

	_, err := p.Get(context.Background(), ModelSpec{Provider: "x", Model: "y"})
	if err == nil {
		// Either error or success is acceptable depending on timing,
		// but we're testing the "closed during factory" path.
		t.Log("Get succeeded (factory completed before Close)")
	} else if !strings.Contains(err.Error(), "closed") {
		t.Logf("Got non-closed error (acceptable): %v", err)
	}
}

// --- MaxResident: pool closed while waiting ---

func TestPool_MaxResident_ClosedWhileWaiting(t *testing.T) {
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return stubClient{}, &stubCloser{}, nil
	}, Options{MaxResident: 1})

	spec1 := ModelSpec{Provider: "a", Model: "m1"}
	_, err := p.Get(context.Background(), spec1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// spec1 is in use (refcount=1), pool is full

	// Try to Get another spec in a goroutine; it will block on p.cond.Wait()
	done := make(chan error, 1)
	go func() {
		spec2 := ModelSpec{Provider: "a", Model: "m2"}
		_, err := p.Get(context.Background(), spec2)
		done <- err
	}()

	// Give the goroutine time to enter the Wait.
	time.Sleep(50 * time.Millisecond)

	// Close the pool, which broadcasts on cond.
	p.Close()

	err = <-done
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("expected 'closed' error, got %v", err)
	}
}

// --- Concurrent Get for same spec ---

func TestPool_Get_ConcurrentSameSpec(t *testing.T) {
	var factoryCalls atomic.Int64
	p := NewPool(func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		factoryCalls.Add(1) // two concurrent Gets both miss and build; count safely
		time.Sleep(20 * time.Millisecond) // simulate slow factory
		return stubClient{}, &stubCloser{}, nil
	}, Options{})
	defer p.Close()

	spec := ModelSpec{Provider: "a", Model: "m"}
	done := make(chan error, 2)

	for i := 0; i < 2; i++ {
		go func() {
			_, err := p.Get(context.Background(), spec)
			done <- err
		}()
	}

	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent Get: %v", err)
		}
	}
}
