package pool

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
)

// fakeClient is a minimal llm.Client used only to identify which spec the
// pool handed back to the caller.
type fakeClient struct {
	provider string
	model    string
}

func (f *fakeClient) Provider() string { return f.provider }
func (f *fakeClient) Model() string    { return f.model }
func (f *fakeClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{}, nil
}

// fakeCloser counts how many times Close() was called.
type fakeCloser struct {
	closed int32
}

func (f *fakeCloser) Close() error {
	atomic.AddInt32(&f.closed, 1)
	return nil
}

// fakeFactory builds fakeClients and counts construction calls per spec.
// Closers are tracked as a slice per spec — concurrent builds for the same
// spec must each get their own closer so the test can verify that the pool
// closed every duplicate after dedupe.
type fakeFactory struct {
	mu        sync.Mutex
	builds    map[string]int
	closers   map[string][]*fakeCloser
	buildErr  error
	buildHook func() // optional callback fired before each build (race tests)
}

func newFakeFactory() *fakeFactory {
	return &fakeFactory{
		builds:  make(map[string]int),
		closers: make(map[string][]*fakeCloser),
	}
}

func (f *fakeFactory) make(_ context.Context, spec ModelSpec) (llm.Client, io.Closer, error) {
	if f.buildHook != nil {
		f.buildHook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.buildErr != nil {
		return nil, nil, f.buildErr
	}
	f.builds[spec.Key()]++
	closer := &fakeCloser{}
	f.closers[spec.Key()] = append(f.closers[spec.Key()], closer)
	return &fakeClient{provider: spec.Provider, model: spec.Model}, closer, nil
}

func (f *fakeFactory) buildCount(spec ModelSpec) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.builds[spec.Key()]
}

// closeCount returns the sum of Close() invocations across every closer
// the factory produced for spec.
func (f *fakeFactory) closeCount(spec ModelSpec) int {
	f.mu.Lock()
	cs := append([]*fakeCloser(nil), f.closers[spec.Key()]...)
	f.mu.Unlock()
	total := 0
	for _, c := range cs {
		total += int(atomic.LoadInt32(&c.closed))
	}
	return total
}

// fakeClock is a controllable clock for TTL tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// ---------- tests ----------

func TestPool_HitMiss(t *testing.T) {
	t.Parallel()
	ff := newFakeFactory()
	p := NewPool(ff.make, Options{})
	defer p.Close()

	spec := ModelSpec{Provider: "anthropic", Model: "claude-sonnet-4-6"}
	ctx := context.Background()

	// First Get → miss.
	c1, err := p.Get(ctx, spec)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := ff.buildCount(spec); got != 1 {
		t.Fatalf("builds after miss=%d, want 1", got)
	}
	// Second Get same spec → hit, no rebuild.
	c2, err := p.Get(ctx, spec)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := ff.buildCount(spec); got != 1 {
		t.Fatalf("builds after second Get=%d, want 1", got)
	}
	if c1 != c2 {
		t.Fatal("expected identical client instance on hit")
	}
	s := p.Stats()
	if s.Hits != 1 || s.Misses != 1 || s.Resident != 1 {
		t.Fatalf("stats: hits=%d misses=%d resident=%d", s.Hits, s.Misses, s.Resident)
	}
	if got := s.HitRate(); got <= 0 || got >= 1 {
		t.Fatalf("hit rate=%v, want strictly between 0 and 1", got)
	}
	p.Release(spec)
	p.Release(spec)
}

func TestPool_LRUEviction(t *testing.T) {
	t.Parallel()
	ff := newFakeFactory()
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	p := NewPool(ff.make, Options{MaxResident: 2, Clock: clk})
	defer p.Close()

	ctx := context.Background()
	a := ModelSpec{Provider: "p", Model: "a"}
	b := ModelSpec{Provider: "p", Model: "b"}
	c := ModelSpec{Provider: "p", Model: "c"}

	if _, err := p.Get(ctx, a); err != nil {
		t.Fatalf("get a: %v", err)
	}
	p.Release(a)
	clk.advance(1 * time.Second)
	if _, err := p.Get(ctx, b); err != nil {
		t.Fatalf("get b: %v", err)
	}
	p.Release(b)
	clk.advance(1 * time.Second)
	// Pool is full and both entries idle. Get(c) must evict the LRU (a).
	if _, err := p.Get(ctx, c); err != nil {
		t.Fatalf("get c: %v", err)
	}
	p.Release(c)
	if ff.closeCount(a) != 1 {
		t.Fatalf("a closeCount=%d, want 1 (LRU evicted)", ff.closeCount(a))
	}
	if ff.closeCount(b) != 0 {
		t.Fatalf("b closeCount=%d, want 0", ff.closeCount(b))
	}
	if s := p.Stats(); s.Resident != 2 || s.Evictions != 1 {
		t.Fatalf("stats: resident=%d evictions=%d", s.Resident, s.Evictions)
	}
}

func TestPool_TTLEviction(t *testing.T) {
	t.Parallel()
	ff := newFakeFactory()
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	// Use a small SweepInterval so the test resolves quickly.
	p := NewPool(ff.make, Options{
		TTL:           50 * time.Millisecond,
		SweepInterval: 5 * time.Millisecond,
		Clock:         clk,
	})
	defer p.Close()

	spec := ModelSpec{Provider: "p", Model: "m"}
	if _, err := p.Get(context.Background(), spec); err != nil {
		t.Fatalf("Get: %v", err)
	}
	p.Release(spec)
	// Advance the clock past TTL and let the sweeper run.
	clk.advance(200 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ff.closeCount(spec) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := ff.closeCount(spec); got != 1 {
		t.Fatalf("closeCount after TTL=%d, want 1", got)
	}
	if s := p.Stats(); s.Resident != 0 {
		t.Fatalf("resident after TTL=%d, want 0", s.Resident)
	}
}

func TestPool_TTLDoesNotEvictPinned(t *testing.T) {
	t.Parallel()
	ff := newFakeFactory()
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	p := NewPool(ff.make, Options{
		TTL:           20 * time.Millisecond,
		SweepInterval: 2 * time.Millisecond,
		Clock:         clk,
	})
	defer p.Close()
	spec := ModelSpec{Provider: "p", Model: "pinned"}
	if _, err := p.Get(context.Background(), spec); err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Don't release — entry stays pinned. Sweeper must NOT close it.
	clk.advance(time.Second)
	time.Sleep(50 * time.Millisecond)
	if got := ff.closeCount(spec); got != 0 {
		t.Fatalf("closeCount for pinned entry=%d, want 0", got)
	}
}

func TestPool_ConcurrentGet_SameSpec(t *testing.T) {
	t.Parallel()
	ff := newFakeFactory()
	const N = 32
	// Use a sync.Barrier-ish gate so every goroutine is past the map check
	// before any factory call finishes. That makes the dedupe path the only
	// way the pool can pass — without dedupe N builds happen and N-1
	// excess closers must be invoked.
	start := make(chan struct{})
	ff.buildHook = func() {
		// Wait for the gate, then sleep a tick so concurrent builds overlap.
		<-start
		time.Sleep(20 * time.Millisecond)
	}
	p := NewPool(ff.make, Options{})
	defer p.Close()

	spec := ModelSpec{Provider: "p", Model: "concurrent"}
	var wg sync.WaitGroup
	clients := make([]llm.Client, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := p.Get(context.Background(), spec)
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			clients[i] = c
		}(i)
	}
	// Give every goroutine a chance to enter the factory before releasing
	// the gate. This is what makes the dedupe behaviour observable.
	time.Sleep(50 * time.Millisecond)
	close(start)
	wg.Wait()
	// At least one client should be returned; all clients should be the
	// same instance (pool deduplicates).
	first := clients[0]
	if first == nil {
		t.Fatal("nil client")
	}
	for i, c := range clients {
		if c != first {
			t.Fatalf("client[%d] != client[0] (pool did not dedupe)", i)
		}
	}
	// The factory may have run more than once if multiple Gets raced past
	// the lock, but the pool keeps the FIRST one in entries. All extra
	// closers must have been closed (no leak).
	if s := p.Stats(); s.Resident != 1 {
		t.Fatalf("resident=%d, want 1", s.Resident)
	}
	builds := ff.buildCount(spec)
	if builds < 1 {
		t.Fatalf("builds=%d, want >=1", builds)
	}
	// Every excess build's closer should have been closed.
	if got, want := ff.closeCount(spec), builds-1; got != want {
		t.Fatalf("close count for excess builds=%d, want %d", got, want)
	}
	for i := 0; i < N; i++ {
		p.Release(spec)
	}
}

func TestPool_RefcountRace(t *testing.T) {
	t.Parallel()
	ff := newFakeFactory()
	p := NewPool(ff.make, Options{})
	defer p.Close()

	spec := ModelSpec{Provider: "p", Model: "refcount"}

	// Pre-warm the cache. This isolates the refcount race from the
	// construction-dedupe race (which is exercised by
	// TestPool_ConcurrentGet_SameSpec). After pre-warm, the N goroutines
	// below all hit the cached entry; no concurrent factory calls happen,
	// so no excess closers should fire.
	if _, err := p.Get(context.Background(), spec); err != nil {
		t.Fatalf("pre-warm Get: %v", err)
	}
	p.Release(spec)

	const N = 200
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Get(context.Background(), spec)
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			p.Release(spec)
		}()
	}
	wg.Wait()
	// After every paired Get/Release, refcount must be zero — the entry is
	// still resident (no TTL), but eligible for eviction.
	if got := ff.closeCount(spec); got != 0 {
		t.Fatalf("closeCount after balanced get/release=%d, want 0", got)
	}
	if s := p.Stats(); s.Resident != 1 {
		t.Fatalf("resident=%d, want 1", s.Resident)
	}
}

func TestPool_MaxResidentBlocksUntilRelease(t *testing.T) {
	t.Parallel()
	ff := newFakeFactory()
	p := NewPool(ff.make, Options{MaxResident: 1})
	defer p.Close()

	ctx := context.Background()
	a := ModelSpec{Provider: "p", Model: "a"}
	b := ModelSpec{Provider: "p", Model: "b"}

	if _, err := p.Get(ctx, a); err != nil {
		t.Fatalf("get a: %v", err)
	}
	// a is pinned. Get(b) must block until we release a.
	done := make(chan error, 1)
	go func() {
		_, err := p.Get(ctx, b)
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("Get(b) returned before Release(a) — MaxResident not enforcing")
	case <-time.After(30 * time.Millisecond):
	}
	p.Release(a)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Get(b): %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Get(b) did not unblock after Release(a)")
	}
}

func TestPool_GetCancelledWhileBlocked(t *testing.T) {
	t.Parallel()
	ff := newFakeFactory()
	p := NewPool(ff.make, Options{MaxResident: 1})
	defer p.Close()

	a := ModelSpec{Provider: "p", Model: "a"}
	b := ModelSpec{Provider: "p", Model: "b"}
	if _, err := p.Get(context.Background(), a); err != nil {
		t.Fatalf("get a: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := p.Get(ctx, b)
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Get returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Get did not unblock after cancel")
	}
	p.Release(a)
}

func TestPool_FactoryErrorSurfaced(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	ff := newFakeFactory()
	ff.buildErr = sentinel
	p := NewPool(ff.make, Options{})
	defer p.Close()

	_, err := p.Get(context.Background(), ModelSpec{Provider: "p", Model: "m"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v, want wraps %v", err, sentinel)
	}
	if s := p.Stats(); s.Resident != 0 {
		t.Fatalf("resident=%d, want 0 after factory error", s.Resident)
	}
}

func TestPool_CloseClosesAllEntries(t *testing.T) {
	t.Parallel()
	ff := newFakeFactory()
	p := NewPool(ff.make, Options{TTL: time.Hour, SweepInterval: time.Hour})
	specs := []ModelSpec{
		{Provider: "p", Model: "x"},
		{Provider: "p", Model: "y"},
		{Provider: "p", Model: "z"},
	}
	for _, s := range specs {
		if _, err := p.Get(context.Background(), s); err != nil {
			t.Fatalf("Get(%s): %v", s.Key(), err)
		}
		p.Release(s)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, s := range specs {
		if ff.closeCount(s) != 1 {
			t.Fatalf("close count for %s=%d, want 1", s.Key(), ff.closeCount(s))
		}
	}
	// Subsequent Get fails.
	if _, err := p.Get(context.Background(), specs[0]); err == nil {
		t.Fatal("Get after Close returned nil error")
	}
}

func TestPool_LoggerReceivesEvents(t *testing.T) {
	t.Parallel()
	ff := newFakeFactory()
	var buf bytes.Buffer
	logger := observability.NewLoggerTo(&buf)
	p := NewPool(ff.make, Options{MaxResident: 1, Logger: logger})
	defer p.Close()

	a := ModelSpec{Provider: "p", Model: "a"}
	b := ModelSpec{Provider: "p", Model: "b"}
	if _, err := p.Get(context.Background(), a); err != nil {
		t.Fatalf("get a: %v", err)
	}
	p.Release(a)
	if _, err := p.Get(context.Background(), b); err != nil {
		t.Fatalf("get b: %v", err)
	}
	p.Release(b)
	out := buf.String()
	if out == "" {
		t.Fatal("logger received no output")
	}
}

func TestModelSpec_Key(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    ModelSpec
		want string
	}{
		{"all", ModelSpec{Provider: "kronk", Model: "llama-3.1-8b", Tag: "Q4_K_M"}, "kronk/llama-3.1-8b@Q4_K_M"},
		{"no-tag", ModelSpec{Provider: "anthropic", Model: "claude-sonnet-4-6"}, "anthropic/claude-sonnet-4-6@"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Key(); got != tc.want {
				t.Fatalf("Key()=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestPool_PanicOnNilFactory(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil factory")
		}
	}()
	NewPool(nil, Options{})
}
