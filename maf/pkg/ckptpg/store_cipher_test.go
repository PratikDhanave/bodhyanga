package ckptpg_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh-maf/pkg/ckptpg"
)

// With a Cipher configured, the payload round-trips through the DB unchanged,
// but the raw data column holds ciphertext — never the plaintext workflow state.
func TestStoreWithCipherEncryptsAtRest(t *testing.T) {
	pool := testPool(t) // skips if no Docker
	cipher, err := ckptpg.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	s := ckptpg.New(pool).WithCipher(cipher)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	payload := json.RawMessage(`{"pending":["physician_review"],"round":2}`)
	info, err := s.CreateCheckpoint(ctx, "sess-enc", payload, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The application-visible read returns the original plaintext.
	got, err := s.RetrieveCheckpoint(ctx, "sess-enc", info)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("decrypted round trip = %s, want %s", got, payload)
	}

	// The raw column must NOT contain the plaintext — it holds a sealed string.
	var raw string
	if err := pool.QueryRow(ctx,
		`SELECT data::text FROM checkpoints WHERE id = $1`, info.CheckpointID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "physician_review") || strings.Contains(raw, "pending") {
		t.Fatalf("data column leaked plaintext at rest: %s", raw)
	}
}
