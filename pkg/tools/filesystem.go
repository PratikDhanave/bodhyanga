package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxReadBytes is the maximum number of bytes returned by ReadFileTool.
// Files larger than this limit are truncated with a notice appended.
const maxReadBytes = 8 * 1024 // 8 KB

// ReadFileTool is a Tool that reads the contents of a file at a given path.
// It is restricted to relative paths and rejects paths containing ".." or
// absolute paths that escape the current working directory.
//
// Input shape (JSON):
//
//	{"path":"relative/path/to/file.txt"}
//
// Output shape (JSON):
//
//	{"path":"relative/path/to/file.txt","content":"...","truncated":false,"bytes":42}
type ReadFileTool struct{}

// Name implements Tool.
func (ReadFileTool) Name() string { return "read_file" }

// Description implements Tool.
func (ReadFileTool) Description() string {
	return "Read the contents of a file at the given relative path. Returns up to 8 KB of content. Rejects absolute paths and any path component containing \"..\"."
}

// Schema implements Tool.
func (ReadFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Relative path to the file to read."
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`)
}

// readFileInput mirrors the JSON shape Invoke accepts.
type readFileInput struct {
	Path string `json:"path"`
}

// readFileOutput is the JSON Invoke returns.
type readFileOutput struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Bytes     int    `json:"bytes"`
}

// Invoke implements Tool.
func (ReadFileTool) Invoke(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("read_file: empty arguments")
	}
	var in readFileInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("read_file: decode arguments: %w", err)
	}
	p := strings.TrimSpace(in.Path)
	if p == "" {
		return nil, fmt.Errorf("read_file: path is empty")
	}
	if err := validatePath(p); err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}

	totalBytes := len(data)
	truncated := false
	if len(data) > maxReadBytes {
		data = data[:maxReadBytes]
		truncated = true
	}

	out := readFileOutput{
		Path:      p,
		Content:   string(data),
		Truncated: truncated,
		Bytes:     totalBytes,
	}
	return json.Marshal(out)
}

// WriteFileTool is a Tool that writes content to a file at a given path.
// Parent directories are created automatically. It rejects absolute paths
// and path components containing "..".
//
// Input shape (JSON):
//
//	{"path":"relative/path/to/file.txt","content":"file contents here"}
//
// Output shape (JSON):
//
//	{"path":"relative/path/to/file.txt","bytes_written":42,"ok":true}
type WriteFileTool struct{}

// Name implements Tool.
func (WriteFileTool) Name() string { return "write_file" }

// Description implements Tool.
func (WriteFileTool) Description() string {
	return "Write content to a file at the given relative path. Creates parent directories as needed. Rejects absolute paths and any path component containing \"..\"."
}

// Schema implements Tool.
func (WriteFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Relative path to the file to write."
    },
    "content": {
      "type": "string",
      "description": "Content to write to the file."
    }
  },
  "required": ["path", "content"],
  "additionalProperties": false
}`)
}

// writeFileInput mirrors the JSON shape Invoke accepts.
type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// writeFileOutput is the JSON Invoke returns.
type writeFileOutput struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytes_written"`
	OK           bool   `json:"ok"`
}

// Invoke implements Tool.
func (WriteFileTool) Invoke(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("write_file: empty arguments")
	}
	var in writeFileInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("write_file: decode arguments: %w", err)
	}
	p := strings.TrimSpace(in.Path)
	if p == "" {
		return nil, fmt.Errorf("write_file: path is empty")
	}
	if err := validatePath(p); err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}

	dir := filepath.Dir(p)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("write_file: create parent dirs: %w", err)
		}
	}

	data := []byte(in.Content)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}

	out := writeFileOutput{
		Path:         p,
		BytesWritten: len(data),
		OK:           true,
	}
	return json.Marshal(out)
}

// ListDirectoryTool is a Tool that lists the entries in a directory at a
// given path. It rejects paths containing "..". The result is a JSON array
// of entry descriptors.
//
// Input shape (JSON):
//
//	{"path":"relative/path/to/dir"}
//
// Output shape (JSON):
//
//	{"path":"relative/path/to/dir","entries":[{"name":"file.txt","is_dir":false,"size_bytes":42},...]}
type ListDirectoryTool struct{}

// Name implements Tool.
func (ListDirectoryTool) Name() string { return "list_directory" }

// Description implements Tool.
func (ListDirectoryTool) Description() string {
	return "List files and sub-directories in the given relative directory path. Returns name, is_dir, and size_bytes for each entry. Rejects any path component containing \"..\"."
}

// Schema implements Tool.
func (ListDirectoryTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Relative path to the directory to list."
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`)
}

// listDirInput mirrors the JSON shape Invoke accepts.
type listDirInput struct {
	Path string `json:"path"`
}

// dirEntry describes one file system entry in the output.
type dirEntry struct {
	Name      string `json:"name"`
	IsDir     bool   `json:"is_dir"`
	SizeBytes int64  `json:"size_bytes"`
}

// listDirOutput is the JSON Invoke returns.
type listDirOutput struct {
	Path    string     `json:"path"`
	Entries []dirEntry `json:"entries"`
}

// Invoke implements Tool.
func (ListDirectoryTool) Invoke(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("list_directory: empty arguments")
	}
	var in listDirInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("list_directory: decode arguments: %w", err)
	}
	p := strings.TrimSpace(in.Path)
	if p == "" {
		return nil, fmt.Errorf("list_directory: path is empty")
	}
	if err := validatePath(p); err != nil {
		return nil, fmt.Errorf("list_directory: %w", err)
	}

	des, err := os.ReadDir(p)
	if err != nil {
		return nil, fmt.Errorf("list_directory: %w", err)
	}

	entries := make([]dirEntry, 0, len(des))
	for _, de := range des {
		var sizeBytes int64
		if !de.IsDir() {
			if info, err2 := de.Info(); err2 == nil {
				sizeBytes = info.Size()
			}
		}
		entries = append(entries, dirEntry{
			Name:      de.Name(),
			IsDir:     de.IsDir(),
			SizeBytes: sizeBytes,
		})
	}

	out := listDirOutput{
		Path:    p,
		Entries: entries,
	}
	return json.Marshal(out)
}

// validatePath returns an error if the path is absolute or contains a ".."
// component. This prevents path traversal attacks and keeps tools scoped to
// the caller's working directory.
func validatePath(p string) error {
	if filepath.IsAbs(p) {
		return fmt.Errorf("absolute paths are not allowed: %q", p)
	}
	// Walk every slash-separated segment looking for "..".
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return fmt.Errorf("path traversal via \"..\" is not allowed: %q", p)
		}
	}
	// filepath.Clean can still produce a rooted path on some platforms if
	// given a clever input — double-check the cleaned result.
	clean := filepath.Clean(p)
	if filepath.IsAbs(clean) {
		return fmt.Errorf("absolute paths are not allowed (cleaned): %q", p)
	}
	return nil
}

// ensure ListDirectoryTool registers the fs.DirEntry interface for compile
// check only (avoids an unused-import if the caller never lists a dir).
var _ fs.DirEntry = nil
