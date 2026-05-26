// kronk_common.go — types shared by kronk_factory.go (build tag `kronk`)
// and kronk_factory_stub.go (build tag `!kronk`).
//
// Keeping KronkOptions / ErrKronkNotBuilt outside the build-tagged files
// means callers in cmd/care can reference them under either build, and
// only the Factory constructor's behaviour swaps based on the tag.

package pool

import "errors"

// ErrKronkNotBuilt is returned by NewKronkFactory when the binary was
// compiled without the `kronk` build tag. cmd/care surfaces this to the
// operator at startup so the misconfiguration is visible before any
// request lands.
var ErrKronkNotBuilt = errors.New(
	"pool: kronk factory not available — rebuild with -tags kronk")

// KronkOptions configures the kronk-backed Factory. Fields are interpreted
// only when the binary was built with the `kronk` build tag; in the default
// build they are accepted but ignored (the Factory returns ErrKronkNotBuilt).
type KronkOptions struct {
	// ModelsDir is the root directory containing .gguf checkpoints. The
	// Factory loads spec.Tag+".gguf" relative to this directory.
	ModelsDir string
	// MaxContextTokens caps the kronk runtime's context window. 0 leaves
	// the runtime default in place.
	MaxContextTokens int
	// GPULayers is the number of model layers offloaded to GPU. 0 means
	// CPU only; -1 (the kronk convention) means "all layers."
	GPULayers int
}
