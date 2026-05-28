// Adapted from https://github.com/ardanlabs/service/blob/master/foundation/web/web.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodh.
//
// Bodh modifications vs upstream:
//   - Stdlib net/http only. The upstream wraps every handler with
//     otelhttp.NewHandler; Bodh keeps the OTel pieces behind the
//     pkg/observability/otel build tag so the default go.sum stays
//     minimal (see AGENTS.md #12, the otel-tagged dep rule).
//   - The Logger signature matches pkg/observability.Logger (Printf-style)
//     rather than the upstream slog-style "Logger.Info(ctx, msg, kv...)"
//     because Bodh routes everything through pkg/phi.LoggerWrapper at the
//     boundary. Adding a structured-log surface here would create a second
//     log seam that the LoggerWrapper would not protect.
//   - HandlerFunc returns `error` rather than `Encoder`. Bodh handlers
//     express "happy path returns nil, anything else returns *errs.Error"
//     because the error envelope is the wire contract. When a handler
//     wants to write a custom success body it does so by returning a
//     Respondable value via Respond directly inside the handler, or by
//     returning a Resp wrapping the encoder. This keeps the signature
//     symmetric with the rest of the Bodh codebase (every agent returns
//     `error`).

package web

import (
	"context"
	"fmt"
	"net/http"
	"path"
)

// Encoder defines anything that can render itself as a byte payload with a
// content type. Both *errs.Error and the Bodh Respondable wrappers satisfy
// this. Returning nil from a HandlerFunc means "no body, web framework
// will write 204 No Content".
type Encoder interface {
	Encode() (data []byte, contentType string, err error)
}

// HandlerFunc is the Bodh handler signature. The handler reads from r,
// returns either a Respondable value (will be JSON-encoded) or an error
// (will be translated to the structured error envelope by pkg/mid.Errors).
//
// The boundary is: handlers MUST NOT touch http.ResponseWriter directly.
// The web framework owns the wire. This is the single biggest divergence
// from net/http idiom; the reward is that every handler is uniformly
// testable and uniformly observable.
type HandlerFunc func(ctx context.Context, r *http.Request) (Encoder, error)

// Logger matches pkg/observability.Logger. The framework writes a single
// line on Respond failure (the only place where the framework itself needs
// to log); everything else goes through middleware.
type Logger interface {
	Printf(format string, v ...any)
}

// App is the Bodh equivalent of a net/http server registry. Hold one per
// process; register routes onto it; pass it to http.ListenAndServe.
type App struct {
	log     Logger
	mux     *http.ServeMux
	mw      []Middleware
	origins []string
}

// NewApp constructs an App pre-wired with the supplied default middleware
// chain. Every handler registered via HandlerFunc inherits this chain;
// per-handler middleware appends to (does not replace) it.
func NewApp(log Logger, mw ...Middleware) *App {
	return &App{
		log: log,
		mux: http.NewServeMux(),
		mw:  mw,
	}
}

// ServeHTTP implements http.Handler. It applies the CORS preamble (if
// EnableCORS was called) and the HSTS preamble, cleans the path, then
// delegates to the registered mux. The path-cleaning step prevents the
// stdlib mux from 301-ing unclean paths into a GET (which would silently
// 405 POST/PUT/DELETE).
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.origins != nil {
		// CORS: only echo allow-origin when the request's Origin appears
		// in the allow-list, never a blanket "*". See MDN's allow-origin
		// guidance.
		reqOrigin := r.Header.Get("Origin")
		for _, origin := range a.origins {
			if origin == "*" || origin == reqOrigin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				break
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST, PATCH, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Request-ID")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	// HSTS: 2-year max-age + preload. Safe to set unconditionally — clients
	// only honour HSTS over HTTPS regardless of header presence on HTTP.
	w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
	// Path cleaning before the mux sees it — see comment in ardanlabs/service.
	r.URL.Path = path.Clean(r.URL.Path)
	a.mux.ServeHTTP(w, r)
}

// EnableCORS opts the app into CORS handling for the supplied allow-list.
// Empty list = CORS disabled entirely (the default).
func (a *App) EnableCORS(origins []string) {
	a.origins = origins
}

// HandlerFunc registers a Bodh-style handler. The framework composes
// middleware around it and wires the http.ResponseWriter into the
// request context so handlers that need it (file streaming, SSE) can
// pull it back out via GetWriter.
func (a *App) HandlerFunc(method, group, pattern string, handler HandlerFunc, mw ...Middleware) {
	// Order of composition: per-handler mw runs INSIDE the default chain
	// (handler is wrapped by per-handler mw first, then by default mw).
	// Looping backwards in wrapMiddleware ensures the slice-leading mw
	// runs first on the inbound side. See pkg/web/middleware.go.
	handler = wrapMiddleware(mw, handler)
	handler = wrapMiddleware(a.mw, handler)

	h := func(w http.ResponseWriter, r *http.Request) {
		ctx := setWriter(r.Context(), w)
		resp, err := handler(ctx, r)
		if err != nil {
			// Errors should normally be intercepted by pkg/mid.Errors,
			// which translates them into a Respondable error envelope.
			// If a handler is registered without that middleware in the
			// chain we fall back to a generic 500 here — the framework
			// must not panic on this path.
			a.log.Printf("web: handler returned uncaught error: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if err := Respond(ctx, w, resp); err != nil {
			a.log.Printf("web: respond failed: %v", err)
			return
		}
	}

	finalPath := pattern
	if group != "" {
		finalPath = "/" + group + pattern
	}
	a.mux.HandleFunc(fmt.Sprintf("%s %s", method, finalPath), h)
}

// RawHandlerFunc registers an http.HandlerFunc unchanged. Used for routes
// that need streaming responses (SSE), embedded asset serving, or
// HL7 ACK responses where the body shape isn't JSON. Middleware still
// runs — the wrapping handler returns a no-op Encoder and the raw
// handler writes the response itself.
func (a *App) RawHandlerFunc(method, group, pattern string, raw http.HandlerFunc, mw ...Middleware) {
	hf := func(ctx context.Context, r *http.Request) (Encoder, error) {
		r = r.WithContext(ctx)
		raw(GetWriter(ctx), r)
		return NoResponse{}, nil
	}
	hf = wrapMiddleware(mw, hf)
	hf = wrapMiddleware(a.mw, hf)

	h := func(w http.ResponseWriter, r *http.Request) {
		ctx := setWriter(r.Context(), w)
		// Errors are intentionally swallowed here — the raw handler owns
		// the response. Middleware-side errors (panics, etc) are still
		// recovered and logged through the chain.
		_, _ = hf(ctx, r)
	}

	finalPath := pattern
	if group != "" {
		finalPath = "/" + group + pattern
	}
	a.mux.HandleFunc(fmt.Sprintf("%s %s", method, finalPath), h)
}

// Handle registers a raw http.Handler with no middleware. Use for
// file servers and other purely-static endpoints.
func (a *App) Handle(pattern string, h http.Handler) {
	a.mux.Handle(pattern, h)
}

// Mux returns the underlying *http.ServeMux. Escape hatch for legacy
// handlers that haven't been ported yet. New code should not call this.
func (a *App) Mux() *http.ServeMux {
	return a.mux
}

// WrapMux applies the App's default middleware chain around an existing
// http.Handler (typically a fully-configured *http.ServeMux that the
// codebase hasn't migrated yet). The returned handler runs the middleware
// chain on every request; the inner handler still writes the response
// itself.
//
// This is the bridge for legacy handlers: it gives them panic recovery,
// the request_id header, and the redacted request log without forcing
// every handler to be rewritten in the new (ctx,r) → (Encoder,error)
// signature. New handlers should be registered via App.HandlerFunc.
func (a *App) WrapMux(inner http.Handler) http.Handler {
	// Adapt the inner handler to a HandlerFunc that always returns
	// NoResponse (the inner handler owns the wire).
	handler := func(ctx context.Context, r *http.Request) (Encoder, error) {
		inner.ServeHTTP(GetWriter(ctx), r.WithContext(ctx))
		return NoResponse{}, nil
	}
	handler = wrapMiddleware(a.mw, handler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := setWriter(r.Context(), w)
		// Errors are intentionally dropped — Errors middleware (if in
		// the chain) translates handler returns into the wire envelope;
		// legacy handlers signal errors via the writer directly.
		_, _ = handler(ctx, r)
	})
}
