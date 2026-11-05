package mid

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/errs"
	"github.com/PratikDhanave/bodhyanga/pkg/web"
)

func TestMetrics_CountsRequestsAndErrors(t *testing.T) {
	cases := []struct {
		name      string
		handler   web.HandlerFunc
		wantErrs  uint64
		wantPanic uint64
	}{
		{
			name: "happy",
			handler: func(ctx context.Context, r *http.Request) (web.Encoder, error) {
				return web.NewJSON(map[string]string{"ok": "1"}), nil
			},
			wantErrs:  0,
			wantPanic: 0,
		},
		{
			name: "typed error",
			handler: func(ctx context.Context, r *http.Request) (web.Encoder, error) {
				return nil, errs.Errorf(errs.NotFound, "x")
			},
			wantErrs:  1,
			wantPanic: 0,
		},
		{
			name: "untyped error",
			handler: func(ctx context.Context, r *http.Request) (web.Encoder, error) {
				return nil, errors.New("kaboom")
			},
			wantErrs:  1,
			wantPanic: 0,
		},
		{
			name: "internal-only-log skips error counter",
			handler: func(ctx context.Context, r *http.Request) (web.Encoder, error) {
				return nil, errs.Errorf(errs.InternalOnlyLog, "panic-shaped")
			},
			wantErrs:  0,
			wantPanic: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCounters()
			wrapped := Metrics()(tc.handler)
			req := httptest.NewRequest("GET", "/x", nil)
			_, _ = wrapped(context.Background(), req)
			snap := Snapshot()
			if snap.Requests != 1 {
				t.Errorf("requests: got %d want 1", snap.Requests)
			}
			if snap.Errors != tc.wantErrs {
				t.Errorf("errors: got %d want %d", snap.Errors, tc.wantErrs)
			}
		})
	}
}
