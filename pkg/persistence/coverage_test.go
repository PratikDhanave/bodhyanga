package persistence

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/eval"
)

// ---- Crypto nil paths ----

func TestEncryptor_NilReceiver(t *testing.T) {
	var enc *Encryptor

	if enc.IsEnabled() {
		t.Error("nil Encryptor.IsEnabled should return false")
	}

	// Encrypt passthrough
	plain := []byte("hello world")
	ct, err := enc.Encrypt(plain)
	if err != nil {
		t.Fatalf("nil Encrypt error: %v", err)
	}
	if string(ct) != string(plain) {
		t.Errorf("nil Encrypt should passthrough: got %q", ct)
	}

	// Decrypt passthrough
	pt, err := enc.Decrypt(ct)
	if err != nil {
		t.Fatalf("nil Decrypt error: %v", err)
	}
	if string(pt) != string(plain) {
		t.Errorf("nil Decrypt should passthrough: got %q", pt)
	}
}

func TestNewEncryptor_WrongKeyLength(t *testing.T) {
	_, err := NewEncryptor([]byte("short"))
	if err == nil {
		t.Error("expected error for short key")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	if !enc.IsEnabled() {
		t.Error("Encryptor should be enabled")
	}

	plain := []byte("sensitive patient data")
	ct, err := enc.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if string(ct) == string(plain) {
		t.Error("ciphertext should differ from plaintext")
	}

	pt, err := enc.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(pt) != string(plain) {
		t.Errorf("Decrypt mismatch: got %q, want %q", pt, plain)
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	key := make([]byte, 32)
	enc, _ := NewEncryptor(key)
	_, err := enc.Decrypt([]byte("short"))
	if err == nil {
		t.Error("expected error for short ciphertext")
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	enc, _ := NewEncryptor(key)

	ct, _ := enc.Encrypt([]byte("secret"))
	// Tamper with the ciphertext
	ct[len(ct)-1] ^= 0xff

	_, err := enc.Decrypt(ct)
	if err == nil {
		t.Error("expected error for tampered ciphertext")
	}
}

func TestNewEncryptorFromEnv_Absent(t *testing.T) {
	os.Unsetenv("BODHYANGA_ENCRYPTION_KEY")
	enc, err := NewEncryptorFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc != nil {
		t.Error("expected nil encryptor when env is absent")
	}
}

func TestNewEncryptorFromEnv_BadBase64(t *testing.T) {
	t.Setenv("BODHYANGA_ENCRYPTION_KEY", "not-valid-base64!!!")
	_, err := NewEncryptorFromEnv()
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

// ---- FileStore Delete (0%) ----

func TestFileStore_Delete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	s.Set("key1", "value1")
	s.Set("key2", "value2")

	v, ok := s.Get("key1")
	if !ok || v != "value1" {
		t.Errorf("expected key1=value1, got %q, %v", v, ok)
	}

	s.Delete("key1")

	_, ok = s.Get("key1")
	if ok {
		t.Error("key1 should be deleted")
	}

	// key2 should still exist.
	v, ok = s.Get("key2")
	if !ok || v != "value2" {
		t.Errorf("key2 should still exist: got %q, %v", v, ok)
	}
}

// ---- FileStore Open with read-only dir ----

func TestFileStore_OpenMkdirFail(t *testing.T) {
	// Try to open a store under a non-existent path inside a read-only dir.
	dir := t.TempDir()
	readonlyDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(readonlyDir, 0o555); err != nil {
		t.Skip("cannot create read-only dir on this platform")
	}
	defer os.Chmod(readonlyDir, 0o755)

	path := filepath.Join(readonlyDir, "subdir", "store.json")
	_, err := Open(path)
	if err == nil {
		t.Error("expected error when mkdir fails")
	}
}

// ---- FileStore Open with empty path ----

func TestFileStore_EmptyPath(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") error: %v", err)
	}
	// Save should be a no-op (flush does nothing with empty path).
	err = s.Save(eval.InteractionRecord{ID: "test"})
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}
	// Verify flush returns nil for empty path.
	s.Set("k", "v")
}

// ---- FileStore Open with invalid JSON ----

func TestFileStore_OpenInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Open(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---- FileStore Open with empty file ----

func TestFileStore_OpenEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open empty file: %v", err)
	}
	if list := s.List(); len(list) != 0 {
		t.Errorf("expected 0 records, got %d", len(list))
	}
}

// ---- FileStore Open with valid JSON but nil KV ----

func TestFileStore_OpenWithNilKV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nilkv.json")
	// Write valid JSON with kv explicitly set to null.
	if err := os.WriteFile(path, []byte(`{"records":[],"kv":null}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// KV should be initialized from nil.
	s.Set("foo", "bar")
	v, ok := s.Get("foo")
	if !ok || v != "bar" {
		t.Errorf("expected foo=bar after init KV")
	}
}

// ---- FileStore Open with non-existent file (file does not exist) ----

func TestFileStore_OpenNonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open non-existent: %v", err)
	}
	if list := s.List(); len(list) != 0 {
		t.Errorf("expected 0 records, got %d", len(list))
	}
}

// ---- FileStore Open with read error (not a dir error) ----

func TestFileStore_OpenReadError(t *testing.T) {
	dir := t.TempDir()
	// Create a directory where the file should be.
	path := filepath.Join(dir, "isdir")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Skip("cannot create dir")
	}
	_, err := Open(path)
	if err == nil {
		t.Error("expected error when path is a directory")
	}
}

// ---- FileStore flush error when path is read-only ----

func TestFileStore_FlushError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Make the file read-only.
	if err := os.WriteFile(path, []byte("{}"), 0o444); err != nil {
		t.Skip("cannot make file read-only")
	}
	defer os.Chmod(path, 0o644)

	err = s.Save(eval.InteractionRecord{ID: "test"})
	if err == nil {
		t.Error("expected error when writing to read-only file")
	}
}
