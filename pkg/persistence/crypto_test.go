package persistence

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// makeKey returns a deterministic 32-byte key for tests.
func makeKey(b byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = b
	}
	return k
}

// TestEncryptDecryptRoundTrip verifies that Encrypt followed by Decrypt
// recovers the original plaintext.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	enc, err := NewEncryptor(makeKey(0x01))
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	cases := []struct {
		name      string
		plaintext []byte
	}{
		{"simple JSON", []byte(`{"patient_id":"pt-001","age":61}`)},
		{"empty", []byte{}},
		{"single byte", []byte{0x42}},
		{"binary zeros", make([]byte, 64)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sealed, err := enc.Encrypt(tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			// Ciphertext must be longer than plaintext (nonce + tag overhead).
			if len(tc.plaintext) > 0 && len(sealed) <= len(tc.plaintext) {
				t.Errorf("sealed length %d not > plaintext %d", len(sealed), len(tc.plaintext))
			}

			got, err := enc.Decrypt(sealed)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if !bytes.Equal(got, tc.plaintext) {
				t.Errorf("round-trip mismatch: got %q, want %q", got, tc.plaintext)
			}
		})
	}
}

// TestEncryptProducesUniqueNonces confirms that two Encrypt calls on the same
// plaintext produce different ciphertexts (probabilistic nonce uniqueness).
func TestEncryptProducesUniqueNonces(t *testing.T) {
	enc, _ := NewEncryptor(makeKey(0x02))
	plaintext := []byte("same message")

	a, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("first Encrypt: %v", err)
	}
	b, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("second Encrypt: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Error("two Encrypt calls produced identical ciphertext — nonce is not random")
	}
}

// TestNilEncryptorPassthrough verifies that a nil *Encryptor returns the
// input unchanged (no-op dev mode).
func TestNilEncryptorPassthrough(t *testing.T) {
	var enc *Encryptor // nil

	if enc.IsEnabled() {
		t.Fatal("nil encryptor should not be enabled")
	}

	plaintext := []byte(`{"phi":"sensitive"}`)

	got, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("nil Encrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("nil Encrypt modified data: %q", got)
	}

	got2, err := enc.Decrypt(plaintext)
	if err != nil {
		t.Fatalf("nil Decrypt: %v", err)
	}
	if !bytes.Equal(got2, plaintext) {
		t.Errorf("nil Decrypt modified data: %q", got2)
	}
}

// TestTamperedCiphertextReturnsError ensures that modifying any byte of the
// sealed blob causes Decrypt to return an authentication error.
func TestTamperedCiphertextReturnsError(t *testing.T) {
	enc, _ := NewEncryptor(makeKey(0x03))
	plaintext := []byte(`{"patient_id":"pt-tamper"}`)

	sealed, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Flip the last byte of the GCM tag.
	tampered := make([]byte, len(sealed))
	copy(tampered, sealed)
	tampered[len(tampered)-1] ^= 0xFF

	if _, err := enc.Decrypt(tampered); err == nil {
		t.Error("Decrypt with tampered ciphertext should return error, got nil")
	}
}

// TestTruncatedCiphertextReturnsError verifies that a too-short blob is
// rejected before attempting decryption.
func TestTruncatedCiphertextReturnsError(t *testing.T) {
	enc, _ := NewEncryptor(makeKey(0x04))

	// Fewer bytes than nonce+tag minimum.
	if _, err := enc.Decrypt([]byte("tooshort")); err == nil {
		t.Error("Decrypt with truncated ciphertext should return error, got nil")
	}
}

// TestWrongKeyReturnsError confirms that decryption with the wrong key fails.
func TestWrongKeyReturnsError(t *testing.T) {
	encA, _ := NewEncryptor(makeKey(0xAA))
	encB, _ := NewEncryptor(makeKey(0xBB))

	sealed, err := encA.Encrypt([]byte("secret PHI"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := encB.Decrypt(sealed); err == nil {
		t.Error("Decrypt with wrong key should return error, got nil")
	}
}

// TestNewEncryptorKeyLength checks that non-32-byte keys are rejected.
func TestNewEncryptorKeyLength(t *testing.T) {
	cases := []int{0, 16, 24, 31, 33, 64}
	for _, n := range cases {
		key := make([]byte, n)
		if _, err := NewEncryptor(key); err == nil {
			t.Errorf("NewEncryptor(%d bytes) should fail, got nil error", n)
		}
	}
	// Exactly 32 bytes must succeed.
	if _, err := NewEncryptor(makeKey(0x00)); err != nil {
		t.Errorf("NewEncryptor(32 bytes) unexpected error: %v", err)
	}
}

// TestNewEncryptorFromEnvNoKey confirms that a missing env var returns
// (nil, nil) for dev-mode passthrough.
func TestNewEncryptorFromEnvNoKey(t *testing.T) {
	t.Setenv("BODH_ENCRYPTION_KEY", "")

	enc, err := NewEncryptorFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc != nil {
		t.Error("expected nil encryptor when env var is absent")
	}
}

// TestNewEncryptorFromEnvValidKey confirms a well-formed base64 key works.
func TestNewEncryptorFromEnvValidKey(t *testing.T) {
	key := makeKey(0xDE)
	encoded := base64.StdEncoding.EncodeToString(key)
	t.Setenv("BODH_ENCRYPTION_KEY", encoded)

	enc, err := NewEncryptorFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc == nil {
		t.Fatal("expected non-nil encryptor")
	}
	if !enc.IsEnabled() {
		t.Error("encryptor should be enabled")
	}
}

// TestNewEncryptorFromEnvBadBase64 confirms that malformed base64 is rejected.
func TestNewEncryptorFromEnvBadBase64(t *testing.T) {
	t.Setenv("BODH_ENCRYPTION_KEY", "not-valid-base64!!!")

	if _, err := NewEncryptorFromEnv(); err == nil {
		t.Error("expected error for malformed base64, got nil")
	}
}
