// keystore.go — in-memory implementation of KeyLookup.
//
// Adapted from foundation/keystore/keystore.go in ardanlabs/service
// (Apache 2.0, Ardan Labs). Modifications for Bodh:
//   - Moved into pkg/auth; Bodh has no `foundation/` directory.
//   - Added GenerateRSAKey helper for cmd/bodhadmin's `keys generate`.
//   - PKCS#1 and PKCS#8 private-key formats are both supported (verbatim
//     from upstream).
//
// Key rotation operates by dropping new .pem files in the directory
// referenced by BODH_AUTH_KEYS_DIR; the keystore loads kids on boot.
// Hot reload is a future enhancement.
package auth
