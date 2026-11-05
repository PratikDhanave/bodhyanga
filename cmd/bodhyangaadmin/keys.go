// Adapted from upstream ardanlabs/service patterns for genkey + gentoken.
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga:
//   - Single binary (not separate cmd/genkey + cmd/gentoken).
//   - Token issue takes a -role flag (repeatable) for Bodhyanga's role catalogue.
//   - No userbus; subjects are opaque strings, not registered users.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/PratikDhanave/bodhyanga/pkg/auth"
)

// runKeys dispatches `bodhyangaadmin keys ...` subcommands.
func runKeys(args []string) {
	if len(args) == 0 {
		log.Fatalf("usage: bodhyangaadmin keys generate|list")
	}
	switch args[0] {
	case "generate":
		runKeysGenerate(args[1:])
	case "list":
		runKeysList(args[1:])
	default:
		log.Fatalf("unknown subcommand %q (want generate|list)", args[0])
	}
}

// runKeysGenerate creates a fresh RSA 2048 keypair and writes the
// private key to <out>/<kid>.pem. A new random kid (UUID without dashes,
// truncated to 12 chars) is generated when -kid is not supplied.
func runKeysGenerate(args []string) {
	fs := flag.NewFlagSet("keys generate", flag.ExitOnError)
	out := fs.String("out", "", "output directory (required)")
	kid := fs.String("kid", "", "key id (filename without extension); default: random")
	_ = fs.Parse(args)
	if *out == "" {
		log.Fatalf("-out <dir> required")
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		log.Fatalf("mkdir %s: %v", *out, err)
	}
	if *kid == "" {
		// 12 chars of a uuid without dashes is enough for collision-free
		// kid names in a dev environment without making the file name
		// unreadable.
		raw := strings.ReplaceAll(uuid.NewString(), "-", "")
		*kid = raw[:12]
	}
	priv, err := auth.GenerateRSAKey(2048)
	if err != nil {
		log.Fatalf("generate key: %v", err)
	}
	path := filepath.Join(*out, *kid+".pem")
	if err := os.WriteFile(path, []byte(priv), 0o600); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
	fmt.Printf("wrote %s (kid=%s)\n", path, *kid)
}

// runKeysList loads all *.pem files under <dir> and prints their kids
// alongside a short SHA-256 fingerprint of the public key (helpful for
// matching a kid against a token's "kid" header during debugging).
func runKeysList(args []string) {
	fs := flag.NewFlagSet("keys list", flag.ExitOnError)
	dir := fs.String("dir", "", "directory holding *.pem (required)")
	_ = fs.Parse(args)
	if *dir == "" {
		log.Fatalf("-dir <dir> required")
	}
	ks := auth.NewKeyStore()
	n, err := ks.LoadByFileSystem(os.DirFS(*dir))
	if err != nil {
		log.Fatalf("load keys: %v", err)
	}
	if n == 0 {
		fmt.Println("(no keys)")
		return
	}
	entries, err := os.ReadDir(*dir)
	if err != nil {
		log.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".pem" {
			continue
		}
		kid := strings.TrimSuffix(e.Name(), ".pem")
		pub, err := ks.PublicKey(kid)
		if err != nil {
			fmt.Printf("%s\t<error: %v>\n", kid, err)
			continue
		}
		sum := sha256.Sum256([]byte(pub))
		fmt.Printf("%s\tsha256:%s\n", kid, hex.EncodeToString(sum[:8]))
	}
}

// runToken dispatches `bodhyangaadmin token ...` subcommands.
func runToken(args []string) {
	if len(args) == 0 {
		log.Fatalf("usage: bodhyangaadmin token issue ...")
	}
	switch args[0] {
	case "issue":
		runTokenIssue(args[1:])
	default:
		log.Fatalf("unknown subcommand %q (want issue)", args[0])
	}
}

// roleList is a flag.Value that collects repeated -role arguments.
type roleList []string

func (r *roleList) String() string { return strings.Join(*r, ",") }
func (r *roleList) Set(v string) error {
	*r = append(*r, v)
	return nil
}

// runTokenIssue mints a signed JWT for the supplied subject + tenant +
// roles. The token's iss matches BODHYANGA_AUTH_ISSUER (or the -issuer flag);
// it must match the cmd/care binary's configured issuer or Parse fails.
func runTokenIssue(args []string) {
	fs := flag.NewFlagSet("token issue", flag.ExitOnError)
	kid := fs.String("kid", "", "key id to sign with (required)")
	dir := fs.String("dir", "", "directory holding the kid's *.pem (required)")
	tenant := fs.String("tenant", "default", "tenant_id claim")
	subject := fs.String("subject", "", "sub claim; default: random uuid")
	ttl := fs.Duration("ttl", time.Hour, "token TTL")
	issuer := fs.String("issuer", "bodhyanga", "iss claim (must match BODHYANGA_AUTH_ISSUER)")
	var roles roleList
	fs.Var(&roles, "role", "role to include in claims (repeatable)")
	_ = fs.Parse(args)
	if *kid == "" || *dir == "" {
		log.Fatalf("-kid and -dir are required")
	}
	if len(roles) == 0 {
		log.Fatalf("at least one -role required (e.g. -role clinician)")
	}
	if *subject == "" {
		*subject = uuid.NewString()
	}
	ks := auth.NewKeyStore()
	if _, err := ks.LoadByFileSystem(os.DirFS(*dir)); err != nil {
		log.Fatalf("load keys: %v", err)
	}
	a := auth.New(auth.Config{KeyLookup: ks, Issuer: *issuer})
	now := time.Now()
	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   *subject,
			Issuer:    *issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(*ttl)),
		},
		TenantID: *tenant,
		Roles:    []string(roles),
	}
	signed, err := a.GenerateToken(*kid, claims)
	if err != nil {
		log.Fatalf("generate token: %v", err)
	}
	// Round-trip the freshly minted token through Parse so operators see
	// a failure here (e.g. issuer mismatch) rather than later at cmd/care.
	if _, err := a.Parse(context.Background(), "Bearer "+signed); err != nil {
		log.Fatalf("self-verify failed (likely issuer mismatch): %v", err)
	}
	fmt.Println(signed)
}
