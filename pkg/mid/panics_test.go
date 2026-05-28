package mid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/errs"
	"github.com/PratikDhanave/bodh/pkg/web"
)

func TestPanics_RecoversAndConvertsToInternalOnlyLog(t *testing.T) {
	cases := []struct {
		name       string
		panicValue any
		wantSubstr string
	}{
		{"string panic", "patient_123 not found", "patient_123 not found"},
		{"error panic", errPanic{"bad state"}, "bad state"},
		{"int panic", 42, "42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetPanicCount()
			handler := func(ctx context.Context, r *http.Request) (web.Encoder, error) {
				panic(tc.panicValue)
			}
			wrapped := Panics()(handler)
			req := httptest.NewRequest("GET", "/x", nil)
			resp, err := wrapped(context.Background(), req)
			if resp != nil {
				t.Errorf("resp should be nil, got %v", resp)
			}
			if err == nil {
				t.Fatal("expected error")
			}
			appErr, ok := errs.As(err)
			if !ok {
				t.Fatalf("expected *errs.Error, got %T", err)
			}
			if !appErr.Code.Equal(errs.InternalOnlyLog) {
				t.Errorf("code: got %v want InternalOnlyLog", appErr.Code)
			}
			if !strings.Contains(appErr.Message, tc.wantSubstr) {
				t.Errorf("message: got %q want substring %q", appErr.Message, tc.wantSubstr)
			}
			if PanicCount() != 1 {
				t.Errorf("panic count: got %d want 1", PanicCount())
			}
		})
	}
}

func TestPanics_NoPanicPasses(t *testing.T) {
	resetPanicCount()
	handler := func(ctx context.Context, r *http.Request) (web.Encoder, error) {
		return web.NewJSON(map[string]string{"ok": "yes"}), nil
	}
	wrapped := Panics()(handler)
	req := httptest.NewRequest("GET", "/x", nil)
	resp, err := wrapped(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp == nil {
		t.Fatal("resp should not be nil")
	}
	if PanicCount() != 0 {
		t.Errorf("panic count should be 0 on happy path")
	}
}

type errPanic struct{ msg string }

func (e errPanic) Error() string { return e.msg }

// Ensure http.Request import is visibly referenced; the inline handler
// signatures already use it via the closure parameter list.
var _ = (*http.Request)(nil)
