// Package pool provides a concurrent-safe, refcounted, optionally
// TTL-evicted cache of llm.Client instances keyed by ModelSpec.
//
// The motivating use case: when Bodh runs on-premise with a local llama.cpp
// stack (kronk), several models share a single GPU — typically a chat
// diagnostician, a vision diagnostician, and an embedding model. GPU memory
// is finite, so we keep N models resident and evict idle ones after a TTL.
//
// The pool itself is provider-agnostic. The actual eviction side-effect
// (unloading from GPU, freeing mmap'd weights) is supplied by the Factory
// the caller registers — for cloud providers, the io.Closer can be a no-op.
//
// See pool_doc.go for the lifecycle diagram and design notes.
package pool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/observability"
)

// ModelSpec is the key the pool uses to identify a unique resident client.
// Provider + Model + Tag must be enough to decide "this is the same model
// loaded the same way." Tag is for per-checkpoint hashes (kronk model files)
// or per-config variants (different temperatures share Provider/Model but
// not Tag would be wrong — temperature is a Request field, not a Spec field).
type ModelSpec struct {
	Provider string
	Model    string
	Tag      string
}

// Key returns the canonical string key for this spec. Used internally by the
// pool's map and exposed for log/metric labels.
func (s ModelSpec) Key() string {
	return s.Provider + "/" + s.Model + "@" + s.Tag
}

// Factory constructs a fresh llm.Client + io.Closer for a given ModelSpec.
//
// The Closer is what the pool calls when it evicts the client; for cloud
// providers it can wrap a no-op. For kronk it unloads the model from GPU.
type Factory func(ctx context.Context, spec ModelSpec) (llm.Client, io.Closer, error)

// Options tunes the pool's behaviour.
type Options struct {
	// MaxResident bounds the number of clients kept in memory. When a new
	// Get would exceed the cap and no entry has refcount 0, Get blocks
	// until one is released. Zero means no bound (cloud providers).
	MaxResident int

	// TTL is the idle duration after which a refcount-0 client is evicted.
	// Zero disables TTL eviction — clients live forever (the default).
	TTL time.Duration

	// SweepInterval is how often the background sweeper checks for expired
	// entries. Defaults to TTL/4 when TTL > 0; ignored when TTL == 0.
	SweepInterval time.Duration

	// Logger receives one-line eviction / construction notices. Optional.
	Logger observability.Logger

	// Clock is injected for tests. Defaults to observability.SystemClock.
	Clock observability.Clock
}

// Stats is a snapshot of pool observability counters.
type Stats struct {
	Resident      int
	Hits          uint64
	Misses        uint64
	Evictions     uint64
	LastEvictTime time.Time
}

// HitRate returns Hits / (Hits + Misses), or 0 when no requests have landed.
func (s Stats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// entry is one resident client in the pool. The pool's mutex covers all
// fields; we deliberately do not use atomics on refcount because admission
// control (MaxResident eviction) must read multiple entries atomically.
type entry struct {
	spec     ModelSpec
	client   llm.Client
	closer   io.Closer
	refcount int
	lastUsed time.Time
}

// Pool is a concurrent-safe cache of llm.Client keyed by ModelSpec.
type Pool struct {
	factory Factory
	opts    Options
	clock   observability.Clock

	mu       sync.Mutex
	entries  map[string]*entry
	stats    Stats
	stopCh   chan struct{}
	sweeperW sync.WaitGroup
	closed   bool

	// cond signals waiters when an entry's refcount drops to zero or when
	// an entry is evicted — that's the only time MaxResident-blocked Get
	// callers should retry.
	cond *sync.Cond
}

// NewPool constructs a pool with the given factory and options.
//
// When opts.TTL > 0, NewPool starts a background sweeper goroutine that runs
// until Close() is called. Callers should always Close() the pool on
// shutdown.
func NewPool(factory Factory, opts Options) *Pool {
	if factory == nil {
		// Refuse to build a pool with no factory — every Get would panic
		// later, far from the misconfiguration site.
		panic("pool: factory must not be nil")
	}
	if opts.Clock == nil {
		opts.Clock = observability.SystemClock{}
	}
	if opts.TTL > 0 && opts.SweepInterval == 0 {
		opts.SweepInterval = opts.TTL / 4
		if opts.SweepInterval < 100*time.Millisecond {
			opts.SweepInterval = 100 * time.Millisecond
		}
	}
	p := &Pool{
		factory: factory,
		opts:    opts,
		clock:   opts.Clock,
		entries: make(map[string]*entry),
		stopCh:  make(chan struct{}),
	}
	p.cond = sync.NewCond(&p.mu)
	if opts.TTL > 0 {
		p.sweeperW.Add(1)
		go p.sweepLoop()
	}
	return p
}

// Get returns a cached llm.Client for the spec, or constructs one via the
// factory. Each successful Get increments the spec's refcount; the caller
// must call Release(spec) when done so the entry becomes eligible for TTL
// eviction.
//
// When MaxResident is set and the pool is full of in-use entries, Get blocks
// until an entry is released or ctx is cancelled.
func (p *Pool) Get(ctx context.Context, spec ModelSpec) (llm.Client, error) {
	key := spec.Key()

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("pool: closed")
	}

	// Fast path: warm hit. Bump refcount, refresh lastUsed.
	if e, ok := p.entries[key]; ok {
		e.refcount++
		e.lastUsed = p.clock.Now()
		p.stats.Hits++
		p.mu.Unlock()
		return e.client, nil
	}

	// Cold path: ensure we can fit. If MaxResident is set and full of in-use
	// entries, either evict an idle one or wait for a release.
	if err := p.makeRoomLocked(ctx); err != nil {
		p.mu.Unlock()
		return nil, err
	}

	p.stats.Misses++
	p.mu.Unlock()

	// Build the client OUTSIDE the lock — factory work can take seconds
	// (GPU load, model mmap) and must not block other Gets on other specs.
	client, closer, err := p.factory(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("pool: factory(%s): %w", key, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		// Pool was closed while we were constructing. Drop the new client
		// on the floor so we don't leak the GPU slot.
		_ = closer.Close()
		return nil, errors.New("pool: closed")
	}
	// Another goroutine may have constructed the same spec concurrently.
	// If so, prefer the existing entry and discard ours.
	if existing, ok := p.entries[key]; ok {
		_ = closer.Close()
		existing.refcount++
		existing.lastUsed = p.clock.Now()
		return existing.client, nil
	}
	e := &entry{
		spec:     spec,
		client:   client,
		closer:   closer,
		refcount: 1,
		lastUsed: p.clock.Now(),
	}
	p.entries[key] = e
	if p.opts.Logger != nil {
		p.opts.Logger.Printf("[llm-pool] construct spec=%s resident=%d", key, len(p.entries))
	}
	return client, nil
}

// Release decrements the refcount for spec. Once the refcount reaches zero
// the entry is eligible for TTL eviction (if TTL > 0) or for MaxResident
// admission of a new spec.
func (p *Pool) Release(spec ModelSpec) {
	key := spec.Key()
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.entries[key]
	if !ok {
		return
	}
	if e.refcount > 0 {
		e.refcount--
	}
	e.lastUsed = p.clock.Now()
	if e.refcount == 0 {
		// Wake any Get blocked on MaxResident.
		p.cond.Broadcast()
	}
}

// Stats returns a snapshot of pool counters.
func (p *Pool) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.stats
	s.Resident = len(p.entries)
	return s
}

// Close stops the sweeper and closes every resident client. Subsequent
// Get calls return an error.
func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.stopCh)
	entries := p.entries
	p.entries = make(map[string]*entry)
	p.cond.Broadcast()
	p.mu.Unlock()

	p.sweeperW.Wait()

	var firstErr error
	for _, e := range entries {
		if err := e.closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// makeRoomLocked is called with p.mu held. If MaxResident is 0 (unbounded)
// it returns immediately. Otherwise it either evicts an idle entry (LRU)
// or blocks on p.cond until one becomes idle or ctx is cancelled.
func (p *Pool) makeRoomLocked(ctx context.Context) error {
	if p.opts.MaxResident <= 0 {
		return nil
	}
	for len(p.entries) >= p.opts.MaxResident {
		// Try LRU eviction of an idle (refcount==0) entry.
		if victim := p.lruIdleLocked(); victim != nil {
			p.evictLocked(victim, "lru")
			continue
		}
		// All entries are in use; wait for a Release. Run a small goroutine
		// that wakes the cond if ctx is cancelled — we cannot select on
		// sync.Cond directly.
		ctxDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				p.mu.Lock()
				p.cond.Broadcast()
				p.mu.Unlock()
			case <-ctxDone:
			}
		}()
		p.cond.Wait()
		close(ctxDone)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p.closed {
			return errors.New("pool: closed")
		}
	}
	return nil
}

// lruIdleLocked returns the least-recently-used entry whose refcount is zero,
// or nil if none exist. Caller holds p.mu.
func (p *Pool) lruIdleLocked() *entry {
	var oldest *entry
	for _, e := range p.entries {
		if e.refcount != 0 {
			continue
		}
		if oldest == nil || e.lastUsed.Before(oldest.lastUsed) {
			oldest = e
		}
	}
	return oldest
}

// evictLocked removes e from the pool and closes its underlying resource.
// Caller holds p.mu. Reason is recorded for logs ("lru", "ttl", "close").
func (p *Pool) evictLocked(e *entry, reason string) {
	delete(p.entries, e.spec.Key())
	p.stats.Evictions++
	p.stats.LastEvictTime = p.clock.Now()
	if p.opts.Logger != nil {
		p.opts.Logger.Printf("[llm-pool] evict spec=%s reason=%s resident=%d",
			e.spec.Key(), reason, len(p.entries))
	}
	// Close outside the lock would be nicer, but eviction during MaxResident
	// admission needs the slot freed atomically; closers should be fast
	// (kronk's Unload returns once the GPU memory is released).
	_ = e.closer.Close()
}

// sweepLoop runs in a background goroutine when TTL > 0, evicting entries
// whose lastUsed is older than TTL and whose refcount is zero.
func (p *Pool) sweepLoop() {
	defer p.sweeperW.Done()
	t := time.NewTicker(p.opts.SweepInterval)
	defer t.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-t.C:
			p.sweepOnce()
		}
	}
}

// sweepOnce evicts every idle entry whose lastUsed is older than TTL.
func (p *Pool) sweepOnce() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	cutoff := p.clock.Now().Add(-p.opts.TTL)
	for _, e := range p.entries {
		if e.refcount == 0 && e.lastUsed.Before(cutoff) {
			p.evictLocked(e, "ttl")
		}
	}
}
