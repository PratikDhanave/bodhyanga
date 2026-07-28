package ckptpg

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func mustKey(t *testing.T, b byte) []byte {
	t.Helper()
	k := make([]byte, 32)
	for i := range k {
		k[i] = b
	}
	return k
}

func TestNewAESGCMRejectsWrongKeySize(t *testing.T) {
	if _, err := NewAESGCM(make([]byte, 16)); err == nil {
		t.Fatal("want error for 16-byte key, got nil")
	}
}

func TestAESGCMRoundTrip(t *testing.T) {
	c, err := NewAESGCM(mustKey(t, 0x01))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"workflow":"state","round":2}`)
	sealed, err := c.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("workflow")) {
		t.Fatal("ciphertext still contains plaintext")
	}
	got, err := c.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip = %q, want %q", got, plain)
	}
}

func TestAESGCMWrongKeyFails(t *testing.T) {
	enc, _ := NewAESGCM(mustKey(t, 0x01))
	dec, _ := NewAESGCM(mustKey(t, 0x02))
	sealed, _ := enc.Seal([]byte("secret"))
	if _, err := dec.Open(sealed); err == nil {
		t.Fatal("want decrypt failure under a different key, got nil")
	}
}

// A fresh nonce per Seal means identical plaintext yields distinct ciphertext.
func TestAESGCMNonceIsRandomized(t *testing.T) {
	c, _ := NewAESGCM(mustKey(t, 0x03))
	a, _ := c.Seal([]byte("same"))
	b, _ := c.Seal([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext produced identical ciphertext")
	}
}

func TestSealOpenPayloadRoundTrip(t *testing.T) {
	c, _ := NewAESGCM(mustKey(t, 0x04))
	orig := json.RawMessage(`{"a":1,"b":[2,3]}`)

	stored, err := sealPayload(c, orig)
	if err != nil {
		t.Fatal(err)
	}
	// Sealed form must be a JSON string (fits JSONB, distinct from plaintext object).
	if !strings.HasPrefix(string(stored), `"`) {
		t.Fatalf("sealed payload is not a JSON string: %s", stored)
	}
	if !json.Valid(stored) {
		t.Fatal("sealed payload is not valid JSON (would break the JSONB column)")
	}

	got, err := openPayload(c, stored)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(orig) {
		t.Fatalf("openPayload = %s, want %s", got, orig)
	}
}

// A legacy plaintext row (a JSON object, not a sealed string) must read back
// unchanged even when a cipher is configured.
func TestOpenPayloadPassesThroughLegacyPlaintext(t *testing.T) {
	c, _ := NewAESGCM(mustKey(t, 0x05))
	legacy := json.RawMessage(`{"legacy":"plaintext"}`)
	got, err := openPayload(c, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(legacy) {
		t.Fatalf("legacy passthrough = %s, want %s", got, legacy)
	}
}
