package mid

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/errs"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/web"
)

func TestLogger_RedactsPHIInLogs(t *testing.T) {
	cases := []struct {
		name        string
		req         *http.Request
		handler     web.HandlerFunc
		notExpected []string
		expected    []string
	}{
		{
			name: "ssn in path is redacted",
			req:  httptest.NewRequest("GET", "/patients/123-45-6789", nil),
			handler: func(ctx context.Context, r *http.Request) (web.Encoder, error) {
				return web.NewJSON(map[string]string{}), nil
			},
			notExpected: []string{"123-45-6789"},
			expected:    []string{"REDACTED-SSN", "code=ok"},
		},
		{
			name: "completed line carries typed code",
			req:  httptest.NewRequest("GET", "/x", nil),
			handler: func(ctx context.Context, r *http.Request) (web.Encoder, error) {
				return nil, errs.Errorf(errs.NotFound, "missing")
			},
			expected: []string{"code=not_found"},
		},
		{
			name: "phone in user-agent is redacted",
			req: func() *http.Request {
				r := httptest.NewRequest("GET", "/x", nil)
				r.Header.Set("User-Agent", "client/555-123-4567")
				return r
			}(),
			handler: func(ctx context.Context, r *http.Request) (web.Encoder, error) {
				return web.NewJSON(map[string]string{}), nil
			},
			notExpected: []string{"555-123-4567"},
			expected:    []string{"REDACTED-PHONE"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			raw := observability.NewLoggerTo(&buf)
			wrapped := Logger(raw)(tc.handler)
			_, _ = wrapped(context.Background(), tc.req)
			out := buf.String()
			for _, s := range tc.notExpected {
				if strings.Contains(out, s) {
					t.Errorf("logs must not contain %q, got: %s", s, out)
				}
			}
			for _, s := range tc.expected {
				if !strings.Contains(out, s) {
					t.Errorf("logs should contain %q, got: %s", s, out)
				}
			}
		})
	}
}
