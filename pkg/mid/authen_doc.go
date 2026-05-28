// authen.go — JWT Bearer-token authentication middleware.
//
// Adapted from app/sdk/mid/authen.go in ardanlabs/service (Apache 2.0,
// Ardan Labs). Modifications for Bodh:
//   - stdlib net/http middleware (not the upstream web.MidFunc shape).
//   - No Basic-auth / userbus path; Bodh does not manage credentials.
//   - Authenticator == nil is a no-op (BODH_AUTH=disabled bypass).
//
// Claims are stashed in the request context under an unexported key;
// downstream handlers access them via mid.Claims(ctx).
package mid
