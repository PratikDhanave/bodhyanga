package observability

import (
	"bufio"
	"net"
	"net/http"
	"testing"
)

// plainRW is a minimal http.ResponseWriter that implements neither
// http.Flusher nor http.Hijacker — used to exercise the fallback paths.
type plainRW struct{ hdr http.Header }

func (p *plainRW) Header() http.Header {
	if p.hdr == nil {
		p.hdr = http.Header{}
	}
	return p.hdr
}
func (p *plainRW) Write(b []byte) (int, error) { return len(b), nil }
func (p *plainRW) WriteHeader(int)             {}

type flushRW struct {
	plainRW
	flushed bool
}

func (f *flushRW) Flush() { f.flushed = true }

type hijackRW struct {
	plainRW
	hijacked bool
}

func (h *hijackRW) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

// TestStatusRecorder_Flush verifies Flush delegates to a Flusher underlying and
// is a no-op (no panic) when the underlying writer is not a Flusher.
func TestStatusRecorder_Flush(t *testing.T) {
	f := &flushRW{}
	(&statusRecorder{ResponseWriter: f}).Flush()
	if !f.flushed {
		t.Fatal("Flush was not delegated to the underlying Flusher")
	}
	// Non-flusher underlying must not panic.
	(&statusRecorder{ResponseWriter: &plainRW{}}).Flush()
}

// TestStatusRecorder_Hijack verifies Hijack delegates to a Hijacker underlying
// and returns an error when the underlying writer is not a Hijacker.
func TestStatusRecorder_Hijack(t *testing.T) {
	h := &hijackRW{}
	if _, _, err := (&statusRecorder{ResponseWriter: h}).Hijack(); err != nil {
		t.Fatalf("Hijack returned error for a Hijacker underlying: %v", err)
	}
	if !h.hijacked {
		t.Fatal("Hijack was not delegated to the underlying Hijacker")
	}
	if _, _, err := (&statusRecorder{ResponseWriter: &plainRW{}}).Hijack(); err == nil {
		t.Fatal("Hijack should error when the underlying writer is not a Hijacker")
	}
}
