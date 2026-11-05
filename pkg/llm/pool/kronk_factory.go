//go:build kronk

// kronk_factory.go — Factory implementation that loads models from the
// local kronk (llama.cpp) runtime.
//
// This file is only compiled when the `kronk` build tag is set. The default
// build uses kronk_factory_stub.go, which returns a clear error. That keeps
// the production go.sum free of kronk's CGO dependencies until an operator
// explicitly opts in.
//
// Layout mirrors kronk's examples/pool/main.go:
//
//   1. Resolve the model file from ModelSpec.Tag → ModelsDir/Tag.gguf.
//   2. Call kronk.LoadModel(ctx, path, runtimeOpts) — returns a handle.
//   3. Wrap the handle in a kronkClient that implements llm.Client.
//   4. Return a Closer that calls handle.Unload() — frees the GPU memory.
//
// The Bodhyanga-side contract is unchanged: Factory returns llm.Client + io.Closer
// + error, and the pool itself doesn't know which provider the spec belongs
// to. Only this file imports the kronk SDK.

package pool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	// NOTE: the kronk SDK import is intentionally left as a comment-only
	// placeholder — the integration track (#21) wires the real package
	// once the kronk Go bindings stabilise. The build tag still gates the
	// file so accidental default builds never compile it.
	//
	// kronk "github.com/ardanlabs/kronk/runtime"
)

// kronkRuntime is the minimal surface this file uses from the kronk SDK.
// Stated as an interface so the integration track can plug in either the
// real runtime or a process-mode shim without touching the pool.
type kronkRuntime interface {
	LoadModel(ctx context.Context, path string, opts kronkRuntimeOptions) (kronkHandle, error)
}

type kronkRuntimeOptions struct {
	MaxContextTokens int
	GPULayers        int
}

type kronkHandle interface {
	Provider() string
	Model() string
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
	Unload() error
}

// runtimeFactory is set by the integration track to return the concrete
// kronk runtime. It's a package-level var so tests under the kronk build
// tag can inject a fake.
var runtimeFactory func() (kronkRuntime, error)

// NewKronkFactory returns a Factory that loads kronk models. The returned
// factory caches a single runtime handle internally — kronk's runtime is
// process-global, so we don't want every Get to spawn a new one.
func NewKronkFactory(opts KronkOptions) Factory {
	if opts.ModelsDir == "" {
		return func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
			return nil, nil, errors.New("pool: KronkOptions.ModelsDir is required")
		}
	}
	if runtimeFactory == nil {
		return func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
			return nil, nil, errors.New(
				"pool: kronk runtimeFactory not registered — integration track #21 pending")
		}
	}
	rt, err := runtimeFactory()
	if err != nil {
		return func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
			return nil, nil, fmt.Errorf("pool: kronk runtime: %w", err)
		}
	}
	return func(ctx context.Context, spec ModelSpec) (llm.Client, io.Closer, error) {
		path := filepath.Join(opts.ModelsDir, spec.Tag+".gguf")
		handle, err := rt.LoadModel(ctx, path, kronkRuntimeOptions{
			MaxContextTokens: opts.MaxContextTokens,
			GPULayers:        opts.GPULayers,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("kronk LoadModel(%s): %w", path, err)
		}
		client := &kronkClient{spec: spec, handle: handle}
		return client, &kronkCloser{handle: handle}, nil
	}
}

// kronkClient adapts a kronk runtime handle to llm.Client.
type kronkClient struct {
	spec   ModelSpec
	handle kronkHandle
}

func (c *kronkClient) Provider() string { return c.spec.Provider }
func (c *kronkClient) Model() string    { return c.spec.Model }
func (c *kronkClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return c.handle.Complete(ctx, req)
}

// kronkCloser unloads the model from kronk's runtime when the pool evicts
// the entry. Errors are returned to the pool which logs them; the entry is
// still removed even if Unload fails (the alternative is a slow leak that
// nothing else recovers from).
type kronkCloser struct {
	handle kronkHandle
}

func (c *kronkCloser) Close() error {
	if c.handle == nil {
		return nil
	}
	return c.handle.Unload()
}
