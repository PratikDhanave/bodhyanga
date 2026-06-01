// Package persistence provides storage backends for Bodh's eval, patient, and
// audit contracts. This file implements package-level encryption for PHI
// columns using AES-256-GCM authenticated encryption.
//
// Key management strategy (envelope encryption):
//
//	Master key  ──►  seals/unseals  ──►  data key (per-column or per-row)
//
// In this initial implementation the master key IS the data key (a single
// 32-byte AES-256 key). A production upgrade replaces NewEncryptorFromEnv
// with a KMS-backed loader (AWS KMS GenerateDataKey, GCP KMS, HashiCorp
// Vault Transit) without changing the Encrypt/Decrypt surface.
//
// Master key source: BODH_ENCRYPTION_KEY env var (base64-encoded 32-byte key).
// For production: swap master key loading to use AWS KMS, GCP KMS, or Vault.
package persistence

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	// aesKeyLen is the required AES-256 key length in bytes.
	aesKeyLen = 32
	// gcmNonceLen is the standard 96-bit (12-byte) GCM nonce.
	gcmNonceLen = 12
)

// Encryptor performs AES-256-GCM authenticated encryption and decryption.
// A nil *Encryptor is a valid no-op passthrough (dev / test mode).
type Encryptor struct {
	key []byte
}

// NewEncryptor constructs an Encryptor from a 32-byte master key.
// Returns an error if masterKey is not exactly 32 bytes.
func NewEncryptor(masterKey []byte) (*Encryptor, error) {
	if len(masterKey) != aesKeyLen {
		return nil, fmt.Errorf("persistence/crypto: master key must be %d bytes, got %d", aesKeyLen, len(masterKey))
	}
	// Copy so the caller cannot mutate our key.
	k := make([]byte, aesKeyLen)
	copy(k, masterKey)
	return &Encryptor{key: k}, nil
}

// NewEncryptorFromEnv reads BODH_ENCRYPTION_KEY (a base64-encoded 32-byte
// key) and returns a ready Encryptor. If the variable is absent, it returns
// (nil, nil) — the nil Encryptor is a safe no-op passthrough suitable for
// local development. Any other error (malformed base64, wrong key length) is
// returned as a fatal startup error.
func NewEncryptorFromEnv() (*Encryptor, error) {
	raw := os.Getenv("BODH_ENCRYPTION_KEY")
	if raw == "" {
		// No-op dev mode: PHI is stored as plaintext bytes. Operators MUST
		// set this variable in all non-development deployments.
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("persistence/crypto: decode BODH_ENCRYPTION_KEY: %w", err)
	}
	return NewEncryptor(key)
}

// IsEnabled reports whether the encryptor will actually encrypt. A nil
// receiver (dev/no-op mode) returns false.
func (e *Encryptor) IsEnabled() bool {
	return e != nil && len(e.key) == aesKeyLen
}

// Encrypt seals plaintext with AES-256-GCM and returns:
//
//	nonce (12 bytes) || ciphertext || GCM tag (16 bytes)
//
// A nil receiver is a passthrough — it returns (plaintext, nil) unchanged.
// This preserves backward-compatibility when the env key is absent.
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	if !e.IsEnabled() {
		return plaintext, nil
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("persistence/crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("persistence/crypto: new gcm: %w", err)
	}

	nonce := make([]byte, gcmNonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("persistence/crypto: generate nonce: %w", err)
	}

	// Seal appends ciphertext+tag to nonce in one allocation.
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return sealed, nil
}

// Decrypt opens an AES-256-GCM sealed blob produced by Encrypt.
// Expected layout: nonce (12 bytes) || ciphertext || GCM tag (16 bytes).
//
// A nil receiver is a passthrough — it returns (ciphertext, nil) as-is,
// treating the stored bytes as plaintext (dev / migration mode).
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	if !e.IsEnabled() {
		return ciphertext, nil
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("persistence/crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("persistence/crypto: new gcm: %w", err)
	}

	minLen := gcmNonceLen + gcm.Overhead()
	if len(ciphertext) < minLen {
		return nil, errors.New("persistence/crypto: ciphertext too short")
	}

	nonce := ciphertext[:gcmNonceLen]
	data := ciphertext[gcmNonceLen:]

	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("persistence/crypto: decrypt: %w", err)
	}
	return plaintext, nil
}
