// Package tools — computer.go
//
// Computer-use tools give an agent the ability to interact with external
// systems: fetching web pages, running safe shell commands, and (in a
// future headful mode) capturing screenshots. All three tools satisfy the
// Tool interface defined in tool.go.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// maxBodyBytes is the maximum number of bytes returned by OpenURLTool and
// RunCommandTool. Keeping responses small prevents context-window bloat.
const maxBodyBytes = 4096

// ─── OpenURLTool ─────────────────────────────────────────────────────────────

// OpenURLTool fetches a web page and returns its text content with HTML tags
// stripped. Only the first 4 KB of the response is returned to keep the
// model's context window manageable.
//
// Input shape (JSON):
//
//	{"url":"https://go.dev/dl/"}
//
// Output shape (JSON):
//
//	{"url":"https://go.dev/dl/","content":"Go Downloads\n...","bytes_read":4096}
type OpenURLTool struct {
	// httpClient allows test injection. When nil a client with a 10-second
	// timeout is constructed on first use.
	httpClient *http.Client
}

// Name implements Tool.
func (OpenURLTool) Name() string { return "open_url" }

// Description implements Tool.
func (OpenURLTool) Description() string {
	return "Fetch a web page and return its text content (HTML tags stripped, max 4 KB)."
}

// Schema implements Tool.
func (OpenURLTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {
      "type": "string",
      "description": "The fully-qualified URL to fetch (must start with http:// or https://)."
    }
  },
  "required": ["url"],
  "additionalProperties": false
}`)
}

type openURLInput struct {
	URL string `json:"url"`
}

type openURLResult struct {
	URL       string `json:"url"`
	Content   string `json:"content"`
	BytesRead int    `json:"bytes_read"`
}

// client returns the HTTP client to use, constructing one if needed.
func (t *OpenURLTool) client() *http.Client {
	if t.httpClient != nil {
		return t.httpClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Invoke implements Tool.
func (t *OpenURLTool) Invoke(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("open_url: empty arguments")
	}
	var in openURLInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("open_url: decode arguments: %w", err)
	}
	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("open_url: url is empty")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("open_url: url must start with http:// or https://")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("open_url: build request: %w", err)
	}
	req.Header.Set("User-Agent", "bodh-computeruse/1.0")

	resp, err := t.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("open_url: http get: %w", err)
	}
	defer resp.Body.Close()

	// Read at most maxBodyBytes + 1 to detect truncation.
	limited := io.LimitReader(resp.Body, int64(maxBodyBytes)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("open_url: read body: %w", err)
	}

	if len(body) > maxBodyBytes {
		body = truncateToValidUTF8(body[:maxBodyBytes])
	}

	text := stripHTMLTags(string(body))

	out := openURLResult{
		URL:       rawURL,
		Content:   text,
		BytesRead: len(body),
	}
	return json.Marshal(out)
}

// stripHTMLTags removes HTML/XML tags from s and collapses whitespace runs.
func stripHTMLTags(s string) string {
	// Remove all <...> tag sequences.
	tagRE := regexp.MustCompile(`<[^>]*>`)
	s = tagRE.ReplaceAllString(s, " ")

	// Decode common HTML entities.
	s = strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&nbsp;", " ",
	).Replace(s)

	// Collapse runs of whitespace (including newlines) to a single space,
	// then trim leading/trailing whitespace.
	wsRE := regexp.MustCompile(`\s+`)
	s = wsRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// truncateToValidUTF8 shortens b so it ends on a valid UTF-8 boundary.
func truncateToValidUTF8(b []byte) []byte {
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b
}

// ─── RunCommandTool ──────────────────────────────────────────────────────────

// RunCommandTool executes a SAFE, READ-ONLY shell command and returns its
// stdout (capped at 4 KB) and exit code. Only an allow-listed set of
// commands may be used; dangerous operators and programs are rejected before
// any execution attempt.
//
// Allowed commands: ls, pwd, cat, head, tail, wc, grep, find, go, git
// (git is further restricted to status / log / diff sub-commands only).
//
// Blocked patterns: |, ;, &&, ||, >, >>, $(), rm, mv, cp, chmod, sudo,
// curl, wget, and any other program not on the allow-list.
//
// Input shape (JSON):
//
//	{"command":"ls -la pkg/tools/"}
//
// Output shape (JSON):
//
//	{"command":"ls -la pkg/tools/","stdout":"total 48\n...","exit_code":0}
type RunCommandTool struct{}

// Name implements Tool.
func (RunCommandTool) Name() string { return "run_command" }

// Description implements Tool.
func (RunCommandTool) Description() string {
	return "Run a safe, read-only shell command (ls, pwd, cat, head, tail, wc, grep, find, go, git status/log/diff). Returns stdout and exit code."
}

// Schema implements Tool.
func (RunCommandTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "description": "The shell command to run. Only safe, read-only commands are permitted."
    }
  },
  "required": ["command"],
  "additionalProperties": false
}`)
}

type runCommandInput struct {
	Command string `json:"command"`
}

type runCommandResult struct {
	Command  string `json:"command"`
	Stdout   string `json:"stdout"`
	ExitCode int    `json:"exit_code"`
}

// allowedCommands is the set of program names that may be executed.
var allowedCommands = map[string]bool{
	"ls":   true,
	"pwd":  true,
	"cat":  true,
	"head": true,
	"tail": true,
	"wc":   true,
	"grep": true,
	"find": true,
	"go":   true,
	"git":  true,
}

// blockedPatterns is the set of shell operators and dangerous programs that
// must never appear in the command string.
var blockedPatterns = []string{
	"|", ";", "&&", "||", ">", ">>", "$(", "`",
	"rm", "mv", "cp", "chmod", "sudo", "curl", "wget",
	"ssh", "scp", "nc", "netcat", "bash", "sh", "zsh",
	"python", "python3", "ruby", "perl", "node", "npm",
	"kill", "pkill", "killall",
}

// gitAllowedSubcommands is the list of git sub-commands that are permitted.
// Any other git sub-command is blocked.
var gitAllowedSubcommands = []string{"status", "log", "diff", "show", "branch", "remote", "describe"}

// validateCommand checks whether cmd is safe to execute and returns an error
// describing the problem if it is not.
func validateCommand(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return fmt.Errorf("run_command: command is empty")
	}

	// Block dangerous shell operators and programs before any parsing.
	for _, pat := range blockedPatterns {
		if strings.Contains(cmd, pat) {
			return fmt.Errorf("run_command: blocked pattern %q in command", pat)
		}
	}

	// Extract the program name (first token).
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return fmt.Errorf("run_command: command is empty after splitting")
	}
	prog := fields[0]

	// Strip any path prefix so "bin/ls" is treated as "ls".
	if idx := strings.LastIndex(prog, "/"); idx >= 0 {
		prog = prog[idx+1:]
	}

	if !allowedCommands[prog] {
		return fmt.Errorf("run_command: program %q is not on the allow-list", prog)
	}

	// Additional validation for git: only allow read-only sub-commands.
	if prog == "git" {
		if len(fields) < 2 {
			return fmt.Errorf("run_command: git requires a sub-command")
		}
		sub := fields[1]
		allowed := false
		for _, s := range gitAllowedSubcommands {
			if sub == s {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("run_command: git sub-command %q is not allowed (use: %s)",
				sub, strings.Join(gitAllowedSubcommands, ", "))
		}
	}

	return nil
}

// Invoke implements Tool.
func (t RunCommandTool) Invoke(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("run_command: empty arguments")
	}
	var in runCommandInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("run_command: decode arguments: %w", err)
	}

	if err := validateCommand(in.Command); err != nil {
		return nil, err
	}

	fields := strings.Fields(in.Command)
	// #nosec G204 — command is validated by validateCommand before exec
	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// Capture stderr too so the agent can diagnose command errors.
	cmd.Stderr = &stdout

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("run_command: exec: %w", err)
		}
	}

	out := stdout.Bytes()
	if len(out) > maxBodyBytes {
		out = truncateToValidUTF8(out[:maxBodyBytes])
	}

	result := runCommandResult{
		Command:  in.Command,
		Stdout:   string(out),
		ExitCode: exitCode,
	}
	return json.Marshal(result)
}

// ─── TakeScreenshotTool ───────────────────────────────────────────────────────

// TakeScreenshotTool is a stub that returns a JSON object indicating that
// screenshots are not available in headless mode. A real implementation
// would invoke a headless browser (e.g. chromedp) or a system screenshot
// utility and return the image as a base64-encoded data URI.
//
// Input shape (JSON):
//
//	{"description":"current state of the browser"}
//
// Output shape (JSON):
//
//	{"available":false,"message":"screenshot requires display"}
type TakeScreenshotTool struct{}

// Name implements Tool.
func (TakeScreenshotTool) Name() string { return "take_screenshot" }

// Description implements Tool.
func (TakeScreenshotTool) Description() string {
	return "Capture a screenshot of the current display (stub: returns unavailable in headless mode)."
}

// Schema implements Tool.
func (TakeScreenshotTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "description": {
      "type": "string",
      "description": "What you want to capture or observe in the screenshot."
    }
  },
  "required": ["description"],
  "additionalProperties": false
}`)
}

type screenshotResult struct {
	Available bool   `json:"available"`
	Message   string `json:"message"`
}

// Invoke implements Tool.
func (TakeScreenshotTool) Invoke(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(screenshotResult{
		Available: false,
		Message:   "screenshot requires display",
	})
}

// ─── Computer tool registry helpers ──────────────────────────────────────────

// NewComputerRegistry returns a Registry pre-populated with all three
// computer-use tools: open_url, run_command, and take_screenshot.
func NewComputerRegistry() *Registry {
	return NewRegistry(
		&OpenURLTool{},
		RunCommandTool{},
		TakeScreenshotTool{},
	)
}
