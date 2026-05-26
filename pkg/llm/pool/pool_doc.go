// pool_doc.go — design notes and lifecycle diagram for the llm client pool.
//
// Overview:
//   The pool keeps llm.Client instances resident across requests. For cloud
//   providers (Anthropic, OpenAI) this is mostly a connection/HTTP-client
//   cache. For on-prem stacks (kronk / llama.cpp) the same shape solves a
//   harder problem: bounded GPU memory shared across several models.
//
// Why a separate package
//   - pkg/llm stays a pure interface package (no goroutines, no state).
//   - pkg/llm/pool is opt-in: agents and cmd/care can construct one and
//     route through it, but the default behaviour of pkg/llm is unchanged.
//   - The kronk-specific Factory lives behind //go:build kronk so default
//     builds never pull in kronk's GPU bindings.
//
// Lifecycle diagram
//
//   caller                                pool                       factory
//     │                                    │                            │
//     │ Get(ctx, spec) ────────────────▶ entries[spec]?                 │
//     │                                    │                            │
//     │                              hit → │ refcount++; lastUsed=now   │
//     │ ◀──────────────────────── client  │                             │
//     │                                    │                            │
//     │                            miss → │  makeRoomLocked()           │
//     │                                    │  (LRU evict / wait)        │
//     │                                    │ ──── factory(ctx, spec) ─▶ │
//     │                                    │ ◀──── client, closer ───── │
//     │ ◀──────────────────────── client  │  refcount=1                 │
//     │                                    │                            │
//     │ ...do work using client...         │                            │
//     │                                    │                            │
//     │ Release(spec) ──────────────────▶ │ refcount--                  │
//     │                                    │ if 0: broadcast cond       │
//     │                                    │                            │
//     │             (background) ─────── sweepLoop                      │
//     │                                    │ every SweepInterval:        │
//     │                                    │   for each idle entry      │
//     │                                    │   older than TTL:          │
//     │                                    │     closer.Close() ──────▶ │
//     │                                    │     delete entries[spec]   │
//     │                                    │                            │
//     │ Close() ─────────────────────────▶ │ stop sweeper               │
//     │                                    │ closer.Close() every entry │
//
// Refcount contract
//   - Get increments refcount, Release decrements. Every Get must be paired
//     with a Release — typically via defer.
//   - Entries with refcount > 0 are pinned: neither TTL nor LRU eviction
//     touches them. This is what makes the pool safe to share across
//     concurrent agent calls.
//   - An entry with refcount == 0 may still be present (warm) until TTL
//     fires or MaxResident admission needs the slot.
//
// MaxResident behaviour
//   - 0 (default) — unbounded. Suitable for cloud providers.
//   - N > 0 — bounded. When Get would exceed the cap:
//       * if any entry has refcount==0, the least-recently-used one is
//         evicted (its closer is called) and Get proceeds.
//       * if every entry is pinned, Get blocks on sync.Cond until a
//         Release fires or ctx is cancelled.
//
// TTL behaviour
//   - 0 (default) — never evict on idle. Suitable for cloud providers.
//   - d > 0 — a background sweeper evicts entries that have been idle
//     (refcount==0) for longer than d. SweepInterval defaults to d/4.
//
// PHI surface
//   The pool only sees ModelSpec (provider + model + tag). It never sees
//   message content, never logs prompts, never persists requests. This is
//   the AGENTS.md invariant #2 (PHI-free audit/log surface) applied to the
//   shared-infra layer.
//
// Where the kronk integration plugs in
//   - kronk_factory.go (build tag `kronk`) supplies a Factory that loads
//     a kronk model from a checkpoint and returns a Closer that unloads it.
//   - kronk_factory_stub.go (build tag `!kronk`) returns a clear error
//     so default builds compile but fail explicitly if someone tries to
//     route a "kronk" spec without the build tag.
//
// See pool.go for the implementation.

package pool
