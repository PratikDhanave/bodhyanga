package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- helpers ---------------------------------------------------------------

// tmpDir creates a temp directory and registers t.Cleanup to remove it.
func tmpDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bodh-fs-test-*")
	if err != nil {
		t.Fatalf("tmpDir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// writeFile creates path (relative to dir) with the given content.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("writeFile mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile write: %v", err)
	}
}

// chdir changes the working directory to dir for the duration of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("chdir getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(old) }) //nolint:errcheck
}

// ---- ReadFileTool ----------------------------------------------------------

func TestReadFileTool_NameDescriptionSchema(t *testing.T) {
	r := ReadFileTool{}
	if r.Name() != "read_file" {
		t.Errorf("Name=%q, want read_file", r.Name())
	}
	if r.Description() == "" {
		t.Error("Description is empty")
	}
	var schema map[string]any
	if err := json.Unmarshal(r.Schema(), &schema); err != nil {
		t.Fatalf("Schema is not valid JSON: %v", err)
	}
}

func TestReadFileTool_ReadsFile(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)
	writeFile(t, dir, "hello.txt", "Hello, World!")

	tool := ReadFileTool{}
	args := json.RawMessage(`{"path":"hello.txt"}`)
	out, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}

	var result readFileOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if result.Content != "Hello, World!" {
		t.Errorf("Content=%q, want %q", result.Content, "Hello, World!")
	}
	if result.Truncated {
		t.Error("Truncated should be false for small file")
	}
	if result.Bytes != 13 {
		t.Errorf("Bytes=%d, want 13", result.Bytes)
	}
}

func TestReadFileTool_TruncatesLargeFile(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	// Write a file bigger than maxReadBytes.
	bigContent := strings.Repeat("x", maxReadBytes+100)
	writeFile(t, dir, "big.txt", bigContent)

	tool := ReadFileTool{}
	args := json.RawMessage(`{"path":"big.txt"}`)
	out, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}

	var result readFileOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !result.Truncated {
		t.Error("Truncated should be true for file > 8 KB")
	}
	if len(result.Content) != maxReadBytes {
		t.Errorf("Content len=%d, want %d", len(result.Content), maxReadBytes)
	}
	if result.Bytes != maxReadBytes+100 {
		t.Errorf("Bytes=%d, want %d", result.Bytes, maxReadBytes+100)
	}
}

func TestReadFileTool_RejectsAbsolutePath(t *testing.T) {
	tool := ReadFileTool{}
	args := json.RawMessage(`{"path":"/etc/passwd"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for absolute path, got nil")
	}
}

func TestReadFileTool_RejectsDotDot(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	tool := ReadFileTool{}
	args := json.RawMessage(`{"path":"../secret.txt"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for path with .., got nil")
	}
}

func TestReadFileTool_ErrorOnMissingFile(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	tool := ReadFileTool{}
	args := json.RawMessage(`{"path":"does_not_exist.txt"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestReadFileTool_EmptyArgs(t *testing.T) {
	tool := ReadFileTool{}
	_, err := tool.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty args, got nil")
	}
}

// ---- WriteFileTool ---------------------------------------------------------

func TestWriteFileTool_NameDescriptionSchema(t *testing.T) {
	w := WriteFileTool{}
	if w.Name() != "write_file" {
		t.Errorf("Name=%q, want write_file", w.Name())
	}
	if w.Description() == "" {
		t.Error("Description is empty")
	}
	var schema map[string]any
	if err := json.Unmarshal(w.Schema(), &schema); err != nil {
		t.Fatalf("Schema is not valid JSON: %v", err)
	}
}

func TestWriteFileTool_WritesFile(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	tool := WriteFileTool{}
	args := json.RawMessage(`{"path":"output.txt","content":"written content"}`)
	out, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}

	var result writeFileOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !result.OK {
		t.Error("OK should be true")
	}
	if result.BytesWritten != len("written content") {
		t.Errorf("BytesWritten=%d, want %d", result.BytesWritten, len("written content"))
	}

	// Verify the file was actually written.
	data, err := os.ReadFile("output.txt")
	if err != nil {
		t.Fatalf("ReadFile after Write: %v", err)
	}
	if string(data) != "written content" {
		t.Errorf("file content=%q, want %q", string(data), "written content")
	}
}

func TestWriteFileTool_CreatesParentDirs(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	tool := WriteFileTool{}
	args := json.RawMessage(`{"path":"subdir/nested/file.txt","content":"deep"}`)
	out, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}

	var result writeFileOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !result.OK {
		t.Error("OK should be true")
	}

	data, err := os.ReadFile("subdir/nested/file.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "deep" {
		t.Errorf("content=%q, want %q", string(data), "deep")
	}
}

func TestWriteFileTool_RejectsAbsolutePath(t *testing.T) {
	tool := WriteFileTool{}
	args := json.RawMessage(`{"path":"/tmp/evil.txt","content":"bad"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for absolute path, got nil")
	}
}

func TestWriteFileTool_RejectsDotDot(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	tool := WriteFileTool{}
	args := json.RawMessage(`{"path":"../escape.txt","content":"bad"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for path with .., got nil")
	}
}

func TestWriteFileTool_EmptyArgs(t *testing.T) {
	tool := WriteFileTool{}
	_, err := tool.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty args, got nil")
	}
}

// ---- ListDirectoryTool -----------------------------------------------------

func TestListDirectoryTool_NameDescriptionSchema(t *testing.T) {
	l := ListDirectoryTool{}
	if l.Name() != "list_directory" {
		t.Errorf("Name=%q, want list_directory", l.Name())
	}
	if l.Description() == "" {
		t.Error("Description is empty")
	}
	var schema map[string]any
	if err := json.Unmarshal(l.Schema(), &schema); err != nil {
		t.Fatalf("Schema is not valid JSON: %v", err)
	}
}

func TestListDirectoryTool_ListsEntries(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	// Create some files and a subdirectory.
	writeFile(t, dir, "alpha.txt", "aaa")
	writeFile(t, dir, "beta.txt", "bbbb")
	if err := os.Mkdir("subdir", 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	tool := ListDirectoryTool{}
	args := json.RawMessage(`{"path":"."}`)
	out, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}

	var result listDirOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	// Build a name → entry map for assertions.
	byName := make(map[string]dirEntry)
	for _, e := range result.Entries {
		byName[e.Name] = e
	}

	if _, ok := byName["alpha.txt"]; !ok {
		t.Error("alpha.txt not in listing")
	}
	if _, ok := byName["beta.txt"]; !ok {
		t.Error("beta.txt not in listing")
	}
	sub, ok := byName["subdir"]
	if !ok {
		t.Fatal("subdir not in listing")
	}
	if !sub.IsDir {
		t.Error("subdir.IsDir should be true")
	}

	alpha := byName["alpha.txt"]
	if alpha.IsDir {
		t.Error("alpha.txt.IsDir should be false")
	}
	if alpha.SizeBytes != 3 {
		t.Errorf("alpha.txt.SizeBytes=%d, want 3", alpha.SizeBytes)
	}
}

func TestListDirectoryTool_RejectsDotDot(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	tool := ListDirectoryTool{}
	args := json.RawMessage(`{"path":".."}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for path with .., got nil")
	}
}

func TestListDirectoryTool_RejectsAbsolutePath(t *testing.T) {
	tool := ListDirectoryTool{}
	args := json.RawMessage(`{"path":"/etc"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for absolute path, got nil")
	}
}

func TestListDirectoryTool_ErrorOnMissingDir(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	tool := ListDirectoryTool{}
	args := json.RawMessage(`{"path":"no_such_dir"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing directory, got nil")
	}
}

func TestListDirectoryTool_EmptyArgs(t *testing.T) {
	tool := ListDirectoryTool{}
	_, err := tool.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty args, got nil")
	}
}

// ---- validatePath ----------------------------------------------------------

func TestValidatePath_AllowsRelative(t *testing.T) {
	cases := []string{
		"foo.txt",
		"subdir/file.txt",
		"a/b/c/d.txt",
		".",
	}
	for _, c := range cases {
		if err := validatePath(c); err != nil {
			t.Errorf("validatePath(%q) = %v, want nil", c, err)
		}
	}
}

func TestValidatePath_RejectsAbsolute(t *testing.T) {
	cases := []string{
		"/etc/passwd",
		"/tmp/foo",
	}
	for _, c := range cases {
		if err := validatePath(c); err == nil {
			t.Errorf("validatePath(%q) = nil, want error", c)
		}
	}
}

func TestValidatePath_RejectsDotDot(t *testing.T) {
	cases := []string{
		"..",
		"../foo",
		"foo/../bar",
		"a/../../etc/passwd",
	}
	for _, c := range cases {
		if err := validatePath(c); err == nil {
			t.Errorf("validatePath(%q) = nil, want error", c)
		}
	}
}
