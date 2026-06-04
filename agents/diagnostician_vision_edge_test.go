package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSniffMediaPNG(t *testing.T) {
	// PNG magic bytes: 89 50 4E 47 0D 0A 1A 0A
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	got := sniffMedia("test.bin", pngData)
	if got != "image/png" {
		t.Errorf("sniffMedia for PNG data = %q", got)
	}
}

func TestSniffMediaJPEG(t *testing.T) {
	// JPEG magic bytes: FF D8 FF
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	got := sniffMedia("test.bin", jpegData)
	if got != "image/jpeg" {
		t.Errorf("sniffMedia for JPEG data = %q", got)
	}
}

func TestSniffMediaByExtensionPNG(t *testing.T) {
	// Data that doesn't match known magic bytes, but has .png extension.
	data := []byte("not actually a png")
	got := sniffMedia("test.png", data)
	if got != "image/png" {
		t.Errorf("sniffMedia for .png extension = %q", got)
	}
}

func TestSniffMediaByExtensionJPG(t *testing.T) {
	data := []byte("not actually a jpeg")
	got := sniffMedia("test.jpg", data)
	if got != "image/jpeg" {
		t.Errorf("sniffMedia for .jpg extension = %q", got)
	}
}

func TestSniffMediaByExtensionJPEG(t *testing.T) {
	data := []byte("not actually a jpeg")
	got := sniffMedia("test.jpeg", data)
	if got != "image/jpeg" {
		t.Errorf("sniffMedia for .jpeg extension = %q", got)
	}
}

func TestSniffMediaUnknown(t *testing.T) {
	data := []byte("not an image")
	got := sniffMedia("test.txt", data)
	if got != "" {
		t.Errorf("sniffMedia for unknown format = %q, want empty", got)
	}
}

func TestExtOf(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"file.png", ".png"},
		{"file.jpg", ".jpg"},
		{"file.JPEG", ".JPEG"},
		{"file", ""},
		{"path/to/file.txt", ".txt"},
		{"noext", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := extOf(tc.path)
		if got != tc.want {
			t.Errorf("extOf(%q)=%q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestReadImageSuccess(t *testing.T) {
	// Create a temp PNG file.
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	// PNG header + minimal IHDR.
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if err := os.WriteFile(path, pngData, 0644); err != nil {
		t.Fatal(err)
	}
	img, err := readImage(path)
	if err != nil {
		t.Fatal(err)
	}
	if img.MediaType != "image/png" {
		t.Errorf("MediaType=%s", img.MediaType)
	}
}

func TestReadImageNotFound(t *testing.T) {
	_, err := readImage("/nonexistent/path/image.png")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadImageUnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bmp")
	if err := os.WriteFile(path, []byte("BM header"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := readImage(path)
	if err == nil {
		t.Error("expected error for unsupported format")
	}
}
