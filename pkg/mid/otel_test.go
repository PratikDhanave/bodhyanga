package mid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/web"
)

func TestOtel_PropagatesRequestID(t *testing.T) {
	cases := []struct {
		name         string
		inboundID    string
		wantNonEmpty bool
		wantExact    string
	}{
		{"generates when missing", "", true, ""},
		{"honours inbound header", "trace-abc-123", true, "trace-abc-123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured string
			handler := func(ctx context.Context, r *http.Request) (web.Encoder, error) {
				captured = web.GetRequestID(ctx)
				return web.NewJSON(map[string]string{}), nil
			}
			wrapped := Otel()(handler)
			req := httptest.NewRequest("GET", "/x", nil)
			if tc.inboundID != "" {
				req.Header.Set(HeaderRequestID, tc.inboundID)
			}
			rr := httptest.NewRecorder()
			ctx := web.SetWriter(req.Context(), rr)
			if _, err := wrapped(ctx, req); err != nil {
				t.Fatalf("wrap: %v", err)
			}
			if tc.wantNonEmpty && captured == "" {
				t.Error("expected non-empty request id")
			}
			if tc.wantExact != "" && captured != tc.wantExact {
				t.Errorf("request id: got %q want %q", captured, tc.wantExact)
			}
			if rr.Header().Get(HeaderRequestID) == "" {
				t.Error("expected response header to be set")
			}
		})
	}
}
