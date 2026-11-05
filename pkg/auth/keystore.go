// Adapted from https://github.com/ardanlabs/service/blob/master/foundation/keystore/keystore.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga:
//   - Moved into pkg/auth (kept as a single package; Bodhyanga has no
//     `foundation/`).
//   - Added GenerateRSAKey helper for cmd/bodhyangaadmin's "generate" path.
//   - Single in-memory implementation; no on-disk loading variant in this
//     adaptation (the upstream LoadByFileSystem path is preserved for
//     parity).

package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"
)

// ErrKeyNotFound is returned by PrivateKey / PublicKey when the kid is
// unknown to the store. The middleware maps this to 401 (not 500): a
// token with an unknown kid is indistinguishable, from the server's
// perspective, from a forgery.
var ErrKeyNotFound = errors.New("key not found")

const maxPEMFileSize = 1024 * 1024 // 1 MiB upper bound per PEM file.

// keyPair is one RSA keypair indexed by kid. Both PEM strings are kept
// so PrivateKey / PublicKey are O(1) without re-parsing.
type keyPair struct {
	privatePEM string
	publicPEM  string
}

// KeyStore is an in-memory implementation of KeyLookup. Safe for
// concurrent reads / writes. Construct via NewKeyStore; populate with
// LoadByFileSystem (production) or LoadRSAPEM (tests / cmd/bodhyangaadmin).
type KeyStore struct {
	mu    sync.RWMutex
	store map[string]keyPair
}

// NewKeyStore returns an empty keystore.
func NewKeyStore() *KeyStore {
	return &KeyStore{store: make(map[string]keyPair)}
}

// LoadRSAPEM registers the given kid with the supplied private-key PEM.
// The public PEM is derived automatically. Used by tests and by
// cmd/bodhyangaadmin's "issue-token" path.
func (ks *KeyStore) LoadRSAPEM(kid, privatePEM string) error {
	pubPEM, err := toPublicPEM(privatePEM)
	if err != nil {
		return fmt.Errorf("derive public PEM for kid %q: %w", kid, err)
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.store[kid] = keyPair{privatePEM: privatePEM, publicPEM: pubPEM}
	return nil
}

// LoadByFileSystem reads every *.pem file in fsys and registers them
// under their filename (sans extension) as kid. Mirrors the upstream
// ardanlabs/service behaviour so operators can drop PEMs into a
// directory and let Bodhyanga pick them up.
func (ks *KeyStore) LoadByFileSystem(fsys fs.FS) (int, error) {
	fn := func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk: %w", err)
		}
		if d.IsDir() {
			return nil
		}
		if path.Ext(name) != ".pem" {
			return nil
		}
		f, err := fsys.Open(name)
		if err != nil {
			return fmt.Errorf("open %q: %w", name, err)
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, maxPEMFileSize))
		if err != nil {
			return fmt.Errorf("read %q: %w", name, err)
		}
		kid := strings.TrimSuffix(d.Name(), ".pem")
		return ks.LoadRSAPEM(kid, string(raw))
	}
	if err := fs.WalkDir(fsys, ".", fn); err != nil {
		return 0, err
	}
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return len(ks.store), nil
}

// PrivateKey returns the private PEM for kid or ErrKeyNotFound.
func (ks *KeyStore) PrivateKey(kid string) (string, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	kp, ok := ks.store[kid]
	if !ok {
		return "", ErrKeyNotFound
	}
	return kp.privatePEM, nil
}

// PublicKey returns the public PEM for kid or ErrKeyNotFound.
func (ks *KeyStore) PublicKey(kid string) (string, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	kp, ok := ks.store[kid]
	if !ok {
		return "", ErrKeyNotFound
	}
	return kp.publicPEM, nil
}

// GenerateRSAKey produces a fresh RSA 2048 keypair encoded as PKCS#1 PEM
// for the private key. Helper for cmd/bodhyangaadmin and for tests that mint
// throwaway keys; production deployments typically pre-generate keys
// out-of-band and drop them in a mounted directory.
func GenerateRSAKey(bits int) (privatePEM string, err error) {
	if bits < 2048 {
		// 2048 is the modern floor; reject smaller silently-weakening keys.
		return "", fmt.Errorf("key size %d below 2048-bit floor", bits)
	}
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", fmt.Errorf("generate rsa key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return string(pemBytes), nil
}

// toPublicPEM derives the public PEM from a PKCS#1 or PKCS#8 private PEM.
// Verbatim from upstream (the public-key derivation is purely
// mechanical) other than the error wording.
func toPublicPEM(privatePEM string) (string, error) {
	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		return "", errors.New("invalid PEM: not a PEM-encoded block")
	}
	var parsed any
	parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("parse private key (tried PKCS1 and PKCS8): %w", err)
		}
	}
	pk, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("key is not an RSA private key")
	}
	asn1Bytes, err := x509.MarshalPKIXPublicKey(&pk.PublicKey)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "PUBLIC KEY", Bytes: asn1Bytes}); err != nil {
		return "", fmt.Errorf("encode public PEM: %w", err)
	}
	return buf.String(), nil
}
