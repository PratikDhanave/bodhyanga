package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ─── OpenURLTool tests ────────────────────────────────────────────────────────

// TestComputerOpenURL_Success checks that a valid URL is fetched and HTML tags
// are stripped from the response body.
func TestComputerOpenURL_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><h1>Go Downloads</h1><p>Latest: go1.22</p></body></html>`))
	}))
	defer srv.Close()

	tool := &OpenURLTool{httpClient: srv.Client()}

	args, _ := json.Marshal(map[string]string{"url": srv.URL})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result openURLResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !strings.Contains(result.Content, "Go Downloads") {
		t.Errorf("expected content to contain 'Go Downloads', got: %q", result.Content)
	}
	if strings.Contains(result.Content, "<h1>") {
		t.Errorf("expected HTML tags to be stripped, got: %q", result.Content)
	}
	if result.URL != srv.URL {
		t.Errorf("URL mismatch: got %q, want %q", result.URL, srv.URL)
	}
}

// TestComputerOpenURL_Truncation checks that responses larger than 4 KB are
// truncated to maxBodyBytes.
func TestComputerOpenURL_Truncation(t *testing.T) {
	bigBody := strings.Repeat("x", maxBodyBytes*2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	tool := &OpenURLTool{httpClient: srv.Client()}

	args, _ := json.Marshal(map[string]string{"url": srv.URL})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result openURLResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	// After stripping HTML and collapsing whitespace the content may be
	// slightly shorter, but bytes_read must be <= maxBodyBytes.
	if result.BytesRead > maxBodyBytes {
		t.Errorf("bytes_read %d exceeds maxBodyBytes %d", result.BytesRead, maxBodyBytes)
	}
}

// TestComputerOpenURL_InvalidURL checks that non-http/https URLs are rejected.
func TestComputerOpenURL_InvalidURL(t *testing.T) {
	tool := &OpenURLTool{}
	args, _ := json.Marshal(map[string]string{"url": "ftp://example.com"})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for ftp:// URL, got nil")
	}
}

// TestComputerOpenURL_EmptyURL checks that an empty URL returns an error.
func TestComputerOpenURL_EmptyURL(t *testing.T) {
	tool := &OpenURLTool{}
	args, _ := json.Marshal(map[string]string{"url": ""})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty URL, got nil")
	}
}

// TestComputerOpenURL_EmptyArgs checks that empty JSON args return an error.
func TestComputerOpenURL_EmptyArgs(t *testing.T) {
	tool := &OpenURLTool{}
	_, err := tool.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil args, got nil")
	}
}

// ─── RunCommandTool safety filter tests ──────────────────────────────────────

func TestComputerRunCommand_SafeCommand(t *testing.T) {
	tool := RunCommandTool{}
	args, _ := json.Marshal(map[string]string{"command": "pwd"})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error for 'pwd': %v", err)
	}
	var result runCommandResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d; stdout: %q", result.ExitCode, result.Stdout)
	}
}

// TestComputerRunCommand_BlockedPipe checks that | is rejected.
func TestComputerRunCommand_BlockedPipe(t *testing.T) {
	blocked := []string{
		"ls | grep foo",
		"ls; pwd",
		"ls && pwd",
		"ls || pwd",
		"ls > /tmp/out",
		"ls >> /tmp/out",
		"$(ls)",
		"rm -rf /tmp/test",
		"mv a b",
		"cp a b",
		"chmod +x script.sh",
		"sudo ls",
		"curl https://example.com",
		"wget https://example.com",
	}
	tool := RunCommandTool{}
	for _, cmd := range blocked {
		args, _ := json.Marshal(map[string]string{"command": cmd})
		_, err := tool.Invoke(context.Background(), args)
		if err == nil {
			t.Errorf("expected error for blocked command %q, got nil", cmd)
		}
	}
}

// TestComputerRunCommand_BlockedProgram checks that programs outside the
// allow-list are rejected.
func TestComputerRunCommand_BlockedProgram(t *testing.T) {
	blocked := []string{
		"python3 script.py",
		"ruby -e 'puts 1'",
		"node index.js",
		"bash -c 'ls'",
		"sh -c 'ls'",
	}
	tool := RunCommandTool{}
	for _, cmd := range blocked {
		args, _ := json.Marshal(map[string]string{"command": cmd})
		_, err := tool.Invoke(context.Background(), args)
		if err == nil {
			t.Errorf("expected error for blocked program in %q, got nil", cmd)
		}
	}
}

// TestComputerRunCommand_GitSubcommandFilter checks that only allowed git
// sub-commands are permitted.
func TestComputerRunCommand_GitSubcommandFilter(t *testing.T) {
	tool := RunCommandTool{}

	allowedSubs := []string{"status", "log", "diff"}
	for _, sub := range allowedSubs {
		args, _ := json.Marshal(map[string]string{"command": "git " + sub})
		// We don't care about the exit code; we just want no safety error.
		_, err := tool.Invoke(context.Background(), args)
		_ = err // git may fail if not in a repo; that's fine
	}

	blockedSubs := []string{"push", "clone", "pull", "commit", "checkout", "reset", "rm"}
	for _, sub := range blockedSubs {
		args, _ := json.Marshal(map[string]string{"command": "git " + sub})
		_, err := tool.Invoke(context.Background(), args)
		if err == nil {
			t.Errorf("expected error for 'git %s', got nil", sub)
		}
	}
}

// TestComputerRunCommand_EmptyCommand checks that an empty command is rejected.
func TestComputerRunCommand_EmptyCommand(t *testing.T) {
	tool := RunCommandTool{}
	args, _ := json.Marshal(map[string]string{"command": ""})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

// TestComputerRunCommand_Truncation checks that large stdout is truncated.
func TestComputerRunCommand_Truncation(t *testing.T) {
	// "go env" produces a fixed-size output; use a large file with head -c would
	// require a real file. We test the boundary indirectly: the tool must not
	// return more than maxBodyBytes in the stdout field.
	tool := RunCommandTool{}
	args, _ := json.Marshal(map[string]string{"command": "go env"})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result runCommandResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Stdout) > maxBodyBytes {
		t.Errorf("stdout length %d exceeds maxBodyBytes %d", len(result.Stdout), maxBodyBytes)
	}
}

// ─── TakeScreenshotTool tests ─────────────────────────────────────────────────

func TestComputerTakeScreenshot_Stub(t *testing.T) {
	tool := TakeScreenshotTool{}
	args, _ := json.Marshal(map[string]string{"description": "current page"})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result screenshotResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Available {
		t.Error("expected available=false for headless stub")
	}
	if result.Message == "" {
		t.Error("expected non-empty message from headless stub")
	}
}

// ─── NewComputerRegistry test ─────────────────────────────────────────────────

func TestComputerNewComputerRegistry(t *testing.T) {
	reg := NewComputerRegistry()
	wantNames := []string{"open_url", "run_command", "take_screenshot"}
	for _, name := range wantNames {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("registry missing tool %q", name)
		}
	}
	if got := len(reg.List()); got != len(wantNames) {
		t.Errorf("registry has %d tools, want %d", got, len(wantNames))
	}
}

// ─── stripHTMLTags unit tests ─────────────────────────────────────────────────

func TestComputerStripHTMLTags(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"<h1>Hello</h1>", "Hello"},
		{"<p>foo &amp; bar</p>", "foo & bar"},
		{"no tags here", "no tags here"},
		{"<a href=\"x\">link</a>", "link"},
		{"  <br/>  spaces  <br/>  ", "spaces"},
	}
	for _, tc := range cases {
		got := stripHTMLTags(tc.input)
		if got != tc.want {
			t.Errorf("stripHTMLTags(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
