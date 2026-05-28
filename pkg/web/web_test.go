package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testLogger captures Printf calls into a bytes.Buffer so tests can
// assert on framework-level diagnostics without involving the std
// logger or stdout.
type testLogger struct{ buf bytes.Buffer }

func (l *testLogger) Printf(format string, v ...any) {
	l.buf.WriteString(format)
}

func TestApp_HandlerFunc_RegistersAndCallsHandler(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		path        string
		handler     HandlerFunc
		reqMethod   string
		reqPath     string
		wantStatus  int
		wantBodyHas string
	}{
		{
			name:   "happy path",
			method: "GET",
			path:   "/ping",
			handler: func(ctx context.Context, r *http.Request) (Encoder, error) {
				return NewJSON(map[string]string{"status": "ok"}), nil
			},
			reqMethod:   "GET",
			reqPath:     "/ping",
			wantStatus:  http.StatusOK,
			wantBodyHas: `"status":"ok"`,
		},
		{
			name:   "no body returns 204",
			method: "GET",
			path:   "/empty",
			handler: func(ctx context.Context, r *http.Request) (Encoder, error) {
				return nil, nil
			},
			reqMethod:  "GET",
			reqPath:    "/empty",
			wantStatus: http.StatusNoContent,
		},
		{
			name:   "uncaught error falls back to 500",
			method: "GET",
			path:   "/boom",
			handler: func(ctx context.Context, r *http.Request) (Encoder, error) {
				return nil, errors.New("kaboom")
			},
			reqMethod:  "GET",
			reqPath:    "/boom",
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := NewApp(&testLogger{})
			app.HandlerFunc(tc.method, "", tc.path, tc.handler)
			req := httptest.NewRequest(tc.reqMethod, tc.reqPath, nil)
			rr := httptest.NewRecorder()
			app.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Errorf("status: got %d want %d (body=%s)", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if tc.wantBodyHas != "" && !strings.Contains(rr.Body.String(), tc.wantBodyHas) {
				t.Errorf("body: got %q want substring %q", rr.Body.String(), tc.wantBodyHas)
			}
		})
	}
}

func TestApp_MiddlewareComposition_Order(t *testing.T) {
	// Verify outer-to-inner inbound order. Slice [A, B, C] should run
	// A → B → C → handler → C → B → A on the way out.
	var trace []string
	mk := func(name string) Middleware {
		return func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, r *http.Request) (Encoder, error) {
				trace = append(trace, name+":in")
				resp, err := next(ctx, r)
				trace = append(trace, name+":out")
				return resp, err
			}
		}
	}
	app := NewApp(&testLogger{}, mk("A"), mk("B"), mk("C"))
	app.HandlerFunc("GET", "", "/x", func(ctx context.Context, r *http.Request) (Encoder, error) {
		trace = append(trace, "handler")
		return NewJSON(map[string]string{}), nil
	})
	req := httptest.NewRequest("GET", "/x", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)
	want := []string{"A:in", "B:in", "C:in", "handler", "C:out", "B:out", "A:out"}
	if len(trace) != len(want) {
		t.Fatalf("trace: got %v want %v", trace, want)
	}
	for i := range want {
		if trace[i] != want[i] {
			t.Errorf("trace[%d]: got %q want %q", i, trace[i], want[i])
		}
	}
}

func TestApp_PerHandlerMiddlewareWrapsInsideDefault(t *testing.T) {
	var trace []string
	mk := func(name string) Middleware {
		return func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, r *http.Request) (Encoder, error) {
				trace = append(trace, name)
				return next(ctx, r)
			}
		}
	}
	app := NewApp(&testLogger{}, mk("default"))
	app.HandlerFunc("GET", "", "/y", func(ctx context.Context, r *http.Request) (Encoder, error) {
		trace = append(trace, "handler")
		return NewJSON(map[string]string{}), nil
	}, mk("perhandler"))
	req := httptest.NewRequest("GET", "/y", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)
	want := []string{"default", "perhandler", "handler"}
	if len(trace) != len(want) {
		t.Fatalf("trace: got %v want %v", trace, want)
	}
	for i := range want {
		if trace[i] != want[i] {
			t.Errorf("trace[%d]: got %q want %q", i, trace[i], want[i])
		}
	}
}

func TestRespond_JSONEncoding(t *testing.T) {
	cases := []struct {
		name        string
		resp        Encoder
		wantStatus  int
		wantBodyHas string
		wantCT      string
	}{
		{"plain json", NewJSON(map[string]int{"n": 7}), http.StatusOK, `"n":7`, "application/json"},
		{"custom status", JSON{Status: http.StatusCreated, Value: map[string]int{"n": 8}}, http.StatusCreated, `"n":8`, "application/json"},
		{"noresponse skips write", NoResponse{}, http.StatusOK, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			if err := Respond(context.Background(), rr, tc.resp); err != nil {
				t.Fatalf("respond: %v", err)
			}
			if _, isNo := tc.resp.(NoResponse); isNo {
				if rr.Code != 200 || rr.Body.Len() != 0 {
					t.Errorf("NoResponse should not touch writer; got code=%d body=%q", rr.Code, rr.Body.String())
				}
				return
			}
			if rr.Code != tc.wantStatus {
				t.Errorf("status: got %d want %d", rr.Code, tc.wantStatus)
			}
			if tc.wantBodyHas != "" && !strings.Contains(rr.Body.String(), tc.wantBodyHas) {
				t.Errorf("body: got %q want substring %q", rr.Body.String(), tc.wantBodyHas)
			}
			if tc.wantCT != "" && rr.Header().Get("Content-Type") != tc.wantCT {
				t.Errorf("content-type: got %q want %q", rr.Header().Get("Content-Type"), tc.wantCT)
			}
		})
	}
}

func TestRespond_NilEncoderReturnsNoContent(t *testing.T) {
	rr := httptest.NewRecorder()
	if err := Respond(context.Background(), rr, nil); err != nil {
		t.Fatalf("respond: %v", err)
	}
	if rr.Code != http.StatusNoContent {
		t.Errorf("status: got %d want 204", rr.Code)
	}
}

func TestRespond_ClientDisconnectShortCircuits(t *testing.T) {
	rr := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Respond(ctx, rr, NewJSON(map[string]string{}))
	if err == nil {
		t.Fatal("expected client-disconnect error")
	}
	if rr.Body.Len() != 0 {
		t.Errorf("body should not be written, got %q", rr.Body.String())
	}
}

func TestDecode_HappyPath(t *testing.T) {
	type body struct {
		Name string `json:"name"`
	}
	r := httptest.NewRequest("POST", "/x", io.NopCloser(strings.NewReader(`{"name":"bob"}`)))
	var b body
	if err := Decode(r, &b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b.Name != "bob" {
		t.Errorf("name: got %q", b.Name)
	}
}

func TestDecode_ValidateHookFires(t *testing.T) {
	type body struct {
		Name string `json:"name"`
	}
	// Validator implemented by a wrapper type so we don't have to touch
	// the inner struct.
	type validated struct{ body }
	_ = validated{} // structural marker only

	r := httptest.NewRequest("POST", "/x", io.NopCloser(strings.NewReader(`{"name":""}`)))
	var v struct {
		body
	}
	// Inline validator: this anonymous struct does NOT satisfy Validator,
	// so decode should pass. The negative case is covered by the
	// requireName fixture below.
	if err := Decode(r, &v); err != nil {
		t.Fatalf("decode: %v", err)
	}

	r = httptest.NewRequest("POST", "/x", io.NopCloser(strings.NewReader(`{"name":""}`)))
	var w requireName
	if err := Decode(r, &w); err == nil {
		t.Fatal("expected validate error for empty name")
	}
}

type requireName struct {
	Name string `json:"name"`
}

func (rn requireName) Validate() error {
	if rn.Name == "" {
		return errors.New("name required")
	}
	return nil
}

func TestContext_RequestIDRoundTrip(t *testing.T) {
	ctx := SetRequestID(context.Background(), "trace-xyz")
	if got := GetRequestID(ctx); got != "trace-xyz" {
		t.Errorf("request id: got %q", got)
	}
}

func TestContext_RequestIDEmptyByDefault(t *testing.T) {
	if got := GetRequestID(context.Background()); got != "" {
		t.Errorf("expected empty request id on bare context, got %q", got)
	}
}

func TestStampStart_Duration(t *testing.T) {
	ctx := StampStart(context.Background())
	if Duration(ctx) < 0 {
		t.Error("duration should be non-negative")
	}
	if Duration(context.Background()) != 0 {
		t.Error("duration on unstamped context should be 0")
	}
}

// Ensure the NoResponse encoder is genuinely no-op.
func TestNoResponse_EncodeReturnsNothing(t *testing.T) {
	data, ct, err := NoResponse{}.Encode()
	if err != nil || len(data) != 0 || ct != "" {
		t.Errorf("got data=%v ct=%q err=%v", data, ct, err)
	}
}

// Sanity: JSON encoder includes a status the response writer picks up.
func TestJSON_HTTPStatusOverride(t *testing.T) {
	j := JSON{Status: http.StatusTeapot, Value: nil}
	rr := httptest.NewRecorder()
	if err := Respond(context.Background(), rr, j); err != nil {
		t.Fatalf("respond: %v", err)
	}
	if rr.Code != http.StatusTeapot {
		t.Errorf("status: got %d want 418", rr.Code)
	}
}

// Final smoke: full JSON envelope is decodable.
func TestRespond_BodyIsValidJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	if err := Respond(context.Background(), rr, NewJSON(map[string]int{"a": 1, "b": 2})); err != nil {
		t.Fatalf("respond: %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["a"] != 1 || got["b"] != 2 {
		t.Errorf("body: got %v", got)
	}
}
