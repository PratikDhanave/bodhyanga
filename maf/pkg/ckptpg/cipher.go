package ckptpg

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Cipher optionally encrypts checkpoint payloads at rest. When a Store has a
// Cipher, each payload is sealed before INSERT and opened after SELECT, so the
// data column never holds plaintext PHI. A nil Cipher stores payloads verbatim
// (the historical behavior) — acceptable only when the database is itself
// encrypted at rest.
type Cipher interface {
	Seal(plaintext []byte) ([]byte, error)
	Open(ciphertext []byte) ([]byte, error)
}

// aesGCM is an AES-256-GCM Cipher. Each Seal draws a fresh random nonce and
// prepends it to the ciphertext.
type aesGCM struct{ aead cipher.AEAD }

// NewAESGCM builds an AES-256-GCM Cipher from a 32-byte key. The caller is
// responsible for the key's provenance and rotation (e.g. a KMS-issued data
// key); this package never logs or persists it.
func NewAESGCM(key []byte) (Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("ckptpg: AES-256 key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("ckptpg: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("ckptpg: new gcm: %w", err)
	}
	return &aesGCM{aead: aead}, nil
}

func (c *aesGCM) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("ckptpg: nonce: %w", err)
	}
	// Seal appends the ciphertext+tag to nonce, yielding nonce||ciphertext.
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (c *aesGCM) Open(ciphertext []byte) ([]byte, error) {
	ns := c.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ckptpg: sealed payload too short")
	}
	nonce, ct := ciphertext[:ns], ciphertext[ns:]
	pt, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		// Never echo ciphertext/plaintext — a wrong key or tampered row only.
		return nil, errors.New("ckptpg: checkpoint decrypt failed (wrong key or corrupt row)")
	}
	return pt, nil
}

// sealPayload wraps sealed bytes as a base64 JSON string so the ciphertext fits
// the JSONB data column and is distinguishable from a legacy plaintext payload
// (a JSON object/array). Round-trips through openPayload.
func sealPayload(c Cipher, data json.RawMessage) (json.RawMessage, error) {
	sealed, err := c.Seal(data)
	if err != nil {
		return nil, err
	}
	// json.Marshal of a string yields a valid JSON string literal.
	return json.Marshal(base64.StdEncoding.EncodeToString(sealed))
}

// openPayload reverses sealPayload. A stored payload that is not a JSON string
// is treated as a legacy plaintext row and returned unchanged, so enabling a
// Cipher never breaks reads of checkpoints written before it existed.
func openPayload(c Cipher, stored json.RawMessage) (json.RawMessage, error) {
	var b64 string
	if err := json.Unmarshal(stored, &b64); err != nil {
		return stored, nil // not a sealed envelope → legacy plaintext
	}
	sealed, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("ckptpg: decode sealed payload: %w", err)
	}
	return c.Open(sealed)
}
