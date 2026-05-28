# Authentication policy for Bodh — JWT signature + issuer validation.
#
# Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/auth/rego/authentication.rego
# Original copyright Ardan Labs (Apache 2.0). Modifications for Bodh:
#   - Package renamed from ardan.rego to bodh.auth.
#   - Algorithm pinned to RS256 in the verify options (defence-in-depth
#     against `alg=none` attacks at the OPA seam, in addition to the
#     jwt/v5 parser's WithValidMethods restriction in the Go layer).
#   - Inline comments to explain each input field.
#
# Input contract (set by pkg/auth.Authenticator.Parse):
#   input.Token  — the raw JWT (no "Bearer " prefix).
#   input.Key    — the PEM-encoded RSA *public* key for the kid in the JWT
#                  header. Looked up by KeyLookup.PublicKey.
#   input.ISS    — the expected issuer string the Authenticator was
#                  configured with at boot. Must equal the `iss` claim.
#
# Output:
#   data.bodh.auth.auth — true iff signature verifies AND iss matches AND
#                         alg == RS256. False on every other path.

package bodh.auth

import rego.v1

default auth := false

auth if {
	[valid, _, _] := io.jwt.decode_verify(input.Token, {
		"cert": input.Key,
		"iss":  input.ISS,
		"alg":  "RS256",
	})
	valid == true
}
