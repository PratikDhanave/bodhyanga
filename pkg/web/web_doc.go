// Package web is the Bodh HTTP framework — a thin layer over net/http that
// gives every handler a uniform shape: take a context+request, return
// (Encoder, error). The framework owns the wire, the handler owns the
// domain logic.
//
// Adapted from Bill Kennedy / Ardan Labs' Ultimate Service template
// (https://github.com/ardanlabs/service), licensed Apache 2.0. See NOTICE
// and LICENSE-APACHE at the repository root. Bodh adapts the framework
// for stdlib net/http only (no opentelemetry-net-http dependency in the
// default build), changes the handler signature to return `error`
// (matching every other Bodh component), and replaces the upstream
// slog-style Logger with the Printf-style pkg/observability.Logger so
// pkg/phi.LoggerWrapper continues to be the single PHI seam.
//
// Why this exists at all (vs. raw net/http): every Bodh HTTP route needs
// the same five things — panic recovery, request log, metric, trace,
// error envelope. Without a framework each handler reimplements them
// (or, worse, forgets one). With a framework the chain is declared once
// in main.go and every route inherits it.
//
// Typical handler:
//
//	func cdmHandler(ctx context.Context, r *http.Request) (web.Encoder, error) {
//	    var req CDMRequest
//	    if err := web.Decode(r, &req); err != nil {
//	        return nil, errs.New(errs.InvalidArgument, err)
//	    }
//	    result, err := svc.HandleCDM(ctx, req)
//	    if err != nil {
//	        return nil, errs.New(errs.Internal, err)
//	    }
//	    return web.NewJSON(result), nil
//	}
//
// The handler never touches http.ResponseWriter. pkg/web.Respond writes
// the body, pkg/mid.Errors translates any non-nil error into the
// {code, message, fields, request_id} envelope documented in docs/api.md.
package web
