// keys.go — bodhadmin "keys generate", "keys list", and "token issue"
// subcommand handlers.
//
// Adapted from upstream ardanlabs/service patterns for genkey + gentoken
// (Apache 2.0, copyright Ardan Labs). The bodhadmin binary collapses
// those two upstream commands into a single binary with subcommands.
//
// Modifications for Bodh:
//   - Token issue takes a repeatable -role flag (clinician, nurse,
//     auditor, …) instead of upstream's binary ADMIN / USER split.
//   - Subjects are opaque strings, not registered users.
//   - Self-verify round-trips every freshly minted token through Parse
//     so misconfiguration (e.g. issuer mismatch) fails at issue time.
package main
