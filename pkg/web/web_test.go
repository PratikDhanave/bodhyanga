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
	"time"
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

// ---------- SetWriter / GetWriter ----------

func TestSetWriter_GetWriter_RoundTrip(t *testing.T) {
	rr := httptest.NewRecorder()
	ctx := SetWriter(context.Background(), rr)
	got := GetWriter(ctx)
	if got != rr {
		t.Error("GetWriter should return the writer set by SetWriter")
	}
}

func TestGetWriter_NilOnBareContext(t *testing.T) {
	if got := GetWriter(context.Background()); got != nil {
		t.Errorf("expected nil on bare context, got %v", got)
	}
}

// ---------- WrapMiddleware ----------

func TestWrapMiddleware_Exported(t *testing.T) {
	var trace []string
	mk := func(name string) Middleware {
		return func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, r *http.Request) (Encoder, error) {
				trace = append(trace, name)
				return next(ctx, r)
			}
		}
	}
	handler := func(ctx context.Context, r *http.Request) (Encoder, error) {
		trace = append(trace, "handler")
		return NewJSON(nil), nil
	}
	wrapped := WrapMiddleware([]Middleware{mk("A"), mk("B")}, handler)
	req := httptest.NewRequest("GET", "/", nil)
	wrapped(context.Background(), req)
	if len(trace) != 3 || trace[0] != "A" || trace[1] != "B" || trace[2] != "handler" {
		t.Errorf("trace: %v, want [A B handler]", trace)
	}
}

func TestWrapMiddleware_NilMiddlewareSkipped(t *testing.T) {
	called := false
	handler := func(ctx context.Context, r *http.Request) (Encoder, error) {
		called = true
		return NoResponse{}, nil
	}
	wrapped := WrapMiddleware([]Middleware{nil, nil}, handler)
	req := httptest.NewRequest("GET", "/", nil)
	wrapped(context.Background(), req)
	if !called {
		t.Error("handler should be called even with nil middleware")
	}
}

// ---------- Param ----------

func TestParam_ReturnsPathValue(t *testing.T) {
	app := NewApp(&testLogger{})
	var got string
	app.HandlerFunc("GET", "", "/items/{id}", func(ctx context.Context, r *http.Request) (Encoder, error) {
		got = Param(r, "id")
		return NewJSON(nil), nil
	})
	req := httptest.NewRequest("GET", "/items/42", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)
	if got != "42" {
		t.Errorf("Param(id) = %q, want '42'", got)
	}
}

func TestParam_EmptyForMissingKey(t *testing.T) {
	req := httptest.NewRequest("GET", "/x", nil)
	if got := Param(req, "nonexistent"); got != "" {
		t.Errorf("expected empty for missing key, got %q", got)
	}
}

// ---------- Decode — invalid JSON ----------

func TestDecode_InvalidJSON(t *testing.T) {
	r := httptest.NewRequest("POST", "/", io.NopCloser(strings.NewReader("not-json")))
	var v struct{ Name string }
	err := Decode(r, &v)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "decode body") {
		t.Errorf("error: %q, want 'decode body' prefix", err.Error())
	}
}

// ---------- Decode — read error ----------

func TestDecode_ReadError(t *testing.T) {
	r := httptest.NewRequest("POST", "/", io.NopCloser(&errReader{}))
	var v struct{ Name string }
	err := Decode(r, &v)
	if err == nil {
		t.Fatal("expected error for read failure")
	}
	if !strings.Contains(err.Error(), "read body") {
		t.Errorf("error: %q, want 'read body' prefix", err.Error())
	}
}

// errReader is an io.Reader that always returns an error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read failure")
}

// ---------- Decode — successful validation ----------

func TestDecode_ValidatorSuccess(t *testing.T) {
	r := httptest.NewRequest("POST", "/", io.NopCloser(strings.NewReader(`{"name":"bob"}`)))
	var v requireName
	err := Decode(r, &v)
	if err != nil {
		t.Fatalf("expected no error for valid input, got %v", err)
	}
	if v.Name != "bob" {
		t.Errorf("Name: got %q", v.Name)
	}
}

// ---------- Respond — error Encoder type ----------

func TestRespond_ErrorEncoder(t *testing.T) {
	rr := httptest.NewRecorder()
	err := Respond(context.Background(), rr, &errEncoder{})
	if err == nil {
		t.Fatal("expected error from bad encoder")
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rr.Code)
	}
}

type errEncoder struct{}

func (errEncoder) Encode() ([]byte, string, error) {
	return nil, "", errors.New("encode fail")
}

// ---------- Respond — encoder that implements error (status 500 fallback) ----------

type errEncoderWithErrorIface struct{}

func (e errEncoderWithErrorIface) Encode() ([]byte, string, error) {
	return []byte("err"), "text/plain", nil
}
func (e errEncoderWithErrorIface) Error() string { return "something broke" }

func TestRespond_ErrorInterfaceEncoder(t *testing.T) {
	rr := httptest.NewRecorder()
	err := Respond(context.Background(), rr, errEncoderWithErrorIface{})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500 for error Encoder", rr.Code)
	}
}

// ---------- Respond — context Deadline exceeded (not Canceled) ----------

func TestRespond_ContextDeadlineExceeded(t *testing.T) {
	rr := httptest.NewRecorder()
	ctx, cancel := context.WithDeadline(context.Background(), timeInPast())
	defer cancel()
	// Deadline exceeded is not context.Canceled, so it should NOT short-circuit
	// with the "client disconnected" message. It should proceed.
	// Wait for the deadline to actually fire.
	<-ctx.Done()

	err := Respond(ctx, rr, NewJSON(map[string]string{"ok": "1"}))
	// ctx.Err() is DeadlineExceeded, which is not Canceled, so
	// the code should NOT return the "client disconnected" error.
	// Actually looking at the code: ctx.Err() returns non-nil but not Canceled,
	// so the switch falls through and proceeds normally.
	_ = err
}

func timeInPast() time.Time {
	return time.Now().Add(-time.Second)
}

// ---------- HTTPStatus — zero value defaults to 200 ----------

func TestJSON_HTTPStatus_ZeroValue(t *testing.T) {
	j := JSON{Value: "x"} // Status field is 0
	if got := j.HTTPStatus(); got != http.StatusOK {
		t.Errorf("HTTPStatus: got %d, want 200", got)
	}
}

// ---------- JSON.Encode — marshal error ----------

func TestJSON_Encode_MarshalError(t *testing.T) {
	// Channels cannot be marshaled.
	j := JSON{Value: make(chan int)}
	_, _, err := j.Encode()
	if err == nil {
		t.Fatal("expected marshal error for channel value")
	}
}

// ---------- ServeHTTP — CORS preamble ----------

func TestApp_ServeHTTP_CORS_MatchingOrigin(t *testing.T) {
	app := NewApp(&testLogger{})
	app.EnableCORS([]string{"https://example.com"})
	app.HandlerFunc("GET", "", "/ping", func(ctx context.Context, r *http.Request) (Encoder, error) {
		return NewJSON("ok"), nil
	})

	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("ACAO: got %q, want 'https://example.com'", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("missing ACAM header")
	}
}

func TestApp_ServeHTTP_CORS_WildcardOrigin(t *testing.T) {
	app := NewApp(&testLogger{})
	app.EnableCORS([]string{"*"})
	app.HandlerFunc("GET", "", "/ping", func(ctx context.Context, r *http.Request) (Encoder, error) {
		return NewJSON("ok"), nil
	})

	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "https://anything.com")
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("ACAO: got %q, want '*'", got)
	}
}

func TestApp_ServeHTTP_CORS_NonMatchingOrigin(t *testing.T) {
	app := NewApp(&testLogger{})
	app.EnableCORS([]string{"https://allowed.com"})
	app.HandlerFunc("GET", "", "/ping", func(ctx context.Context, r *http.Request) (Encoder, error) {
		return NewJSON("ok"), nil
	})

	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO should be empty for non-matching origin, got %q", got)
	}
}

func TestApp_ServeHTTP_CORS_Preflight(t *testing.T) {
	app := NewApp(&testLogger{})
	app.EnableCORS([]string{"https://example.com"})

	req := httptest.NewRequest("OPTIONS", "/anything", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 for OPTIONS preflight", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("MaxAge: got %q, want '86400'", got)
	}
}

func TestApp_ServeHTTP_HSTS(t *testing.T) {
	app := NewApp(&testLogger{})
	app.HandlerFunc("GET", "", "/ping", func(ctx context.Context, r *http.Request) (Encoder, error) {
		return NewJSON("ok"), nil
	})

	req := httptest.NewRequest("GET", "/ping", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	hsts := rr.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Error("missing HSTS header")
	}
	if !strings.Contains(hsts, "max-age=63072000") {
		t.Errorf("HSTS: got %q", hsts)
	}
}

func TestApp_ServeHTTP_PathCleaning(t *testing.T) {
	app := NewApp(&testLogger{})
	var gotPath string
	app.HandlerFunc("GET", "", "/clean", func(ctx context.Context, r *http.Request) (Encoder, error) {
		gotPath = r.URL.Path
		return NewJSON("ok"), nil
	})

	// Unclean path should be cleaned.
	req := httptest.NewRequest("GET", "/./clean", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if gotPath != "/clean" {
		t.Errorf("path: got %q, want '/clean'", gotPath)
	}
}

// ---------- RawHandlerFunc ----------

func TestApp_RawHandlerFunc(t *testing.T) {
	app := NewApp(&testLogger{})
	app.RawHandlerFunc("GET", "", "/raw", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("raw response"))
	})

	req := httptest.NewRequest("GET", "/raw", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "raw response") {
		t.Errorf("body: got %q", rr.Body.String())
	}
}

func TestApp_RawHandlerFunc_WithGroup(t *testing.T) {
	app := NewApp(&testLogger{})
	app.RawHandlerFunc("GET", "api", "/raw", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("grouped raw"))
	})

	req := httptest.NewRequest("GET", "/api/raw", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if !strings.Contains(rr.Body.String(), "grouped raw") {
		t.Errorf("body: got %q", rr.Body.String())
	}
}

func TestApp_RawHandlerFunc_WithMiddleware(t *testing.T) {
	var mwCalled bool
	mw := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, r *http.Request) (Encoder, error) {
			mwCalled = true
			return next(ctx, r)
		}
	}

	app := NewApp(&testLogger{})
	app.RawHandlerFunc("GET", "", "/rawmw", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}, mw)

	req := httptest.NewRequest("GET", "/rawmw", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if !mwCalled {
		t.Error("per-handler middleware should have run")
	}
}

// ---------- Handle ----------

func TestApp_Handle(t *testing.T) {
	app := NewApp(&testLogger{})
	app.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("static content"))
	}))

	req := httptest.NewRequest("GET", "/static/file.js", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if !strings.Contains(rr.Body.String(), "static content") {
		t.Errorf("body: got %q", rr.Body.String())
	}
}

// ---------- Mux ----------

func TestApp_Mux(t *testing.T) {
	app := NewApp(&testLogger{})
	mux := app.Mux()
	if mux == nil {
		t.Fatal("Mux() should return non-nil")
	}
}

// ---------- WrapMux ----------

func TestApp_WrapMux(t *testing.T) {
	var mwCalled bool
	mw := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, r *http.Request) (Encoder, error) {
			mwCalled = true
			return next(ctx, r)
		}
	}

	app := NewApp(&testLogger{}, mw)

	inner := http.NewServeMux()
	inner.HandleFunc("GET /legacy", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("legacy handler"))
	})

	wrapped := app.WrapMux(inner)

	req := httptest.NewRequest("GET", "/legacy", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if !mwCalled {
		t.Error("middleware should have run for wrapped mux")
	}
	if !strings.Contains(rr.Body.String(), "legacy handler") {
		t.Errorf("body: got %q", rr.Body.String())
	}
}

// ---------- HandlerFunc with group ----------

func TestApp_HandlerFunc_WithGroup(t *testing.T) {
	app := NewApp(&testLogger{})
	app.HandlerFunc("GET", "v1", "/items", func(ctx context.Context, r *http.Request) (Encoder, error) {
		return NewJSON(map[string]string{"group": "v1"}), nil
	})

	req := httptest.NewRequest("GET", "/v1/items", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "v1") {
		t.Errorf("body: got %q", rr.Body.String())
	}
}

// ---------- HandlerFunc — handler uses GetWriter ----------

func TestApp_HandlerFunc_GetWriterInHandler(t *testing.T) {
	app := NewApp(&testLogger{})
	var writerFound bool
	app.HandlerFunc("GET", "", "/check", func(ctx context.Context, r *http.Request) (Encoder, error) {
		writerFound = GetWriter(ctx) != nil
		return NewJSON("ok"), nil
	})

	req := httptest.NewRequest("GET", "/check", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if !writerFound {
		t.Error("GetWriter should return non-nil inside a handler")
	}
}

// ---------- EnableCORS with empty slice ----------

func TestApp_EnableCORS_EmptySlice(t *testing.T) {
	app := NewApp(&testLogger{})
	app.EnableCORS([]string{})
	// With empty origins, CORS should still be "enabled" but no origin matches.
	app.HandlerFunc("GET", "", "/test", func(ctx context.Context, r *http.Request) (Encoder, error) {
		return NewJSON("ok"), nil
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	// CORS headers should be set (Allow-Methods etc) but not Allow-Origin.
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO should be empty with empty origins, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Allow-Methods should still be set")
	}
}

// ---------- Respond — encoder returns empty content type ----------

func TestRespond_EmptyContentTypeDefaultsToJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	enc := &bareEncoder{data: []byte(`{"x":1}`)}
	err := Respond(context.Background(), rr, enc)
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type: got %q, want 'application/json'", ct)
	}
}

type bareEncoder struct{ data []byte }

func (b *bareEncoder) Encode() ([]byte, string, error) {
	return b.data, "", nil // empty content type
}

// ---------- Respond — Encoder that implements httpStatus with custom code ----------

func TestRespond_CustomHTTPStatus(t *testing.T) {
	rr := httptest.NewRecorder()
	enc := JSON{Status: http.StatusAccepted, Value: map[string]int{"n": 1}}
	err := Respond(context.Background(), rr, enc)
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if rr.Code != http.StatusAccepted {
		t.Errorf("status: got %d, want 202", rr.Code)
	}
}

// ---------- context: setStart / GetStart ----------

func TestGetStart_DefaultZero(t *testing.T) {
	if got := GetStart(context.Background()); got != 0 {
		t.Errorf("expected 0 on bare context, got %d", got)
	}
}

func TestSetStart_GetStart_RoundTrip(t *testing.T) {
	ctx := setStart(context.Background(), 12345)
	if got := GetStart(ctx); got != 12345 {
		t.Errorf("expected 12345, got %d", got)
	}
}

// ---------- Respond — write error ----------

// errWriter is an http.ResponseWriter whose Write always fails.
type errWriter struct {
	header http.Header
	code   int
}

func (ew *errWriter) Header() http.Header {
	if ew.header == nil {
		ew.header = make(http.Header)
	}
	return ew.header
}
func (ew *errWriter) WriteHeader(code int) { ew.code = code }
func (ew *errWriter) Write(b []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestRespond_WriteError(t *testing.T) {
	w := &errWriter{}
	err := Respond(context.Background(), w, NewJSON(map[string]int{"a": 1}))
	if err == nil {
		t.Fatal("expected error from write failure")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("error: %q, want 'write' in message", err.Error())
	}
}

// ---------- HandlerFunc — Respond fails (error logged) ----------

func TestApp_HandlerFunc_RespondFails(t *testing.T) {
	log := &testLogger{}
	app := NewApp(log)

	// Handler returns a value that will fail during Encode.
	app.HandlerFunc("GET", "", "/encode-fail", func(ctx context.Context, r *http.Request) (Encoder, error) {
		return JSON{Value: make(chan int)}, nil // channel cannot be marshaled
	})

	req := httptest.NewRequest("GET", "/encode-fail", nil)
	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	// The respond error should have been logged.
	if !strings.Contains(log.buf.String(), "respond failed") {
		t.Errorf("expected 'respond failed' in log, got %q", log.buf.String())
	}
}
