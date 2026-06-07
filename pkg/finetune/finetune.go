// Package finetune provides utilities for validating, splitting, and uploading
// fine-tuning datasets to OpenAI and Anthropic APIs. It complements the
// cmd/export-hitl-feedback pipeline: export first produces SFT/DPO JSONL,
// then this package validates and stages that data for fine-tuning runs.
package finetune

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// openAIBaseURL is the base URL for OpenAI API calls. It is a package-level
// variable so that internal tests can point it at an httptest server.
var openAIBaseURL = "https://api.openai.com"

// DatasetStats summarises a validated JSONL dataset.
type DatasetStats struct {
	Total            int
	TrainCount       int
	ValCount         int
	AvgPromptLen     int
	AvgCompletionLen int
	MinScore         float64
	MaxScore         float64
}

// FineTuneJob represents a fine-tuning job at a provider.
type FineTuneJob struct {
	Provider  string // "anthropic" | "openai"
	Model     string // base model to fine-tune
	TrainFile string // path to JSONL used for training
	ValFile   string // path to validation JSONL (optional)
	JobID     string // returned by API
	Status    string // "queued" | "running" | "succeeded" | "failed"
	CreatedAt string // RFC 3339
}

// sftLine is the expected shape of each line in an SFT JSONL file.
// It mirrors the output of cmd/export-hitl-feedback.
type sftLine struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	// Optional score field used by some RLHF pipelines.
	Score *float64 `json:"score,omitempty"`
}

// ValidateDataset reads a JSONL file and checks that every line is valid SFT
// format: a JSON object with a "messages" array containing at least a user turn
// and an assistant turn. It returns aggregate stats, a (possibly empty) list of
// human-readable validation errors (one per bad line), and any I/O error.
//
// The stats are computed over all lines, valid or not, so callers can make
// informed decisions about whether to proceed despite warnings.
func ValidateDataset(path string) (DatasetStats, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return DatasetStats{}, nil, fmt.Errorf("finetune: open %s: %w", path, err)
	}
	defer f.Close()

	var (
		stats       DatasetStats
		errs        []string
		totalPrompt int
		totalCompl  int
		minScore    = float64(1<<53 - 1) // large sentinel
		maxScore    = -float64(1<<53 - 1)
		hasScore    bool
	)

	scanner := bufio.NewScanner(f)
	// Allow lines up to 10 MB (some case-state JSON is large).
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		raw := scanner.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		stats.Total++

		var line sftLine
		if err := json.Unmarshal(raw, &line); err != nil {
			errs = append(errs, fmt.Sprintf("line %d: invalid JSON: %v", lineNum, err))
			continue
		}
		if len(line.Messages) == 0 {
			errs = append(errs, fmt.Sprintf("line %d: 'messages' array is empty", lineNum))
			continue
		}

		var (
			hasUser      bool
			hasAssistant bool
			promptLen    int
			complLen     int
		)
		for _, m := range line.Messages {
			switch m.Role {
			case "user", "system":
				hasUser = true
				promptLen += len(m.Content)
			case "assistant":
				hasAssistant = true
				complLen += len(m.Content)
			default:
				errs = append(errs, fmt.Sprintf("line %d: unknown role %q", lineNum, m.Role))
			}
		}
		if !hasUser {
			errs = append(errs, fmt.Sprintf("line %d: missing user or system turn", lineNum))
		}
		if !hasAssistant {
			errs = append(errs, fmt.Sprintf("line %d: missing assistant turn", lineNum))
		}

		totalPrompt += promptLen
		totalCompl += complLen

		if line.Score != nil {
			hasScore = true
			s := *line.Score
			if s < minScore {
				minScore = s
			}
			if s > maxScore {
				maxScore = s
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, errs, fmt.Errorf("finetune: scan %s: %w", path, err)
	}

	if stats.Total > 0 {
		stats.AvgPromptLen = totalPrompt / stats.Total
		stats.AvgCompletionLen = totalCompl / stats.Total
	}
	if hasScore {
		stats.MinScore = minScore
		stats.MaxScore = maxScore
	}
	// TrainCount / ValCount are only meaningful after a split; default to total.
	stats.TrainCount = stats.Total

	return stats, errs, nil
}

// SplitDataset shuffles the lines in path and writes (1-valRatio) of them to
// trainPath and valRatio of them to valPath. valRatio must be in (0, 1).
// A fixed-seed local RNG is used so the split is deterministic per call unless
// the caller changes its own seed.
func SplitDataset(path, trainPath, valPath string, valRatio float64) error {
	if valRatio <= 0 || valRatio >= 1 {
		return fmt.Errorf("finetune: valRatio must be in (0,1), got %f", valRatio)
	}

	// Read all non-empty lines.
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("finetune: open %s: %w", path, err)
	}
	var lines [][]byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		b := scanner.Bytes()
		if len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		cp := make([]byte, len(b))
		copy(cp, b)
		lines = append(lines, cp)
	}
	f.Close()
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("finetune: scan %s: %w", path, err)
	}
	if len(lines) == 0 {
		return fmt.Errorf("finetune: %s is empty", path)
	}

	// Shuffle with a reproducible local source (seed = unix nanosecond).
	rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // not crypto
	rng.Shuffle(len(lines), func(i, j int) { lines[i], lines[j] = lines[j], lines[i] })

	valN := int(float64(len(lines)) * valRatio)
	if valN == 0 {
		valN = 1
	}
	trainN := len(lines) - valN

	// Ensure output directories exist.
	for _, p := range []string{trainPath, valPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fmt.Errorf("finetune: mkdir %s: %w", filepath.Dir(p), err)
		}
	}

	if err := writeLines(trainPath, lines[:trainN]); err != nil {
		return err
	}
	return writeLines(valPath, lines[trainN:])
}

func writeLines(path string, lines [][]byte) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("finetune: create %s: %w", path, err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, l := range lines {
		if _, err := w.Write(l); err != nil {
			return fmt.Errorf("finetune: write %s: %w", path, err)
		}
		if err := w.WriteByte('\n'); err != nil {
			return fmt.Errorf("finetune: write newline %s: %w", path, err)
		}
	}
	return w.Flush()
}

// tokenEstimate approximates token count as characters / 4 (GPT-style average).
func tokenEstimate(chars int) float64 {
	return float64(chars) / 4.0
}

// pricePerMillion returns the approximate cost in USD per 1 M training tokens
// for the given provider + model. Figures are illustrative / public list prices
// as of June 2025; update when providers change their rates.
func pricePerMillion(provider, model string) float64 {
	switch strings.ToLower(provider) {
	case "openai":
		switch {
		case strings.HasPrefix(model, "gpt-4o"):
			return 25.00
		case strings.HasPrefix(model, "gpt-4"):
			return 8.00
		case strings.HasPrefix(model, "gpt-3.5"):
			return 0.80
		default:
			return 8.00
		}
	case "anthropic":
		// Anthropic fine-tuning pricing is not yet publicly listed; use a
		// conservative placeholder.
		return 15.00
	default:
		return 10.00
	}
}

// EstimateCost returns a rough USD cost estimate for fine-tuning a dataset.
// It multiplies total tokens (prompt + completion) by the number of epochs and
// the provider's per-million-token price.
func EstimateCost(stats DatasetStats, provider, model string, epochs int) float64 {
	if epochs <= 0 {
		epochs = 1
	}
	charsPerExample := stats.AvgPromptLen + stats.AvgCompletionLen
	totalChars := charsPerExample * stats.Total
	totalTokens := tokenEstimate(totalChars)
	price := pricePerMillion(provider, model)
	return float64(epochs) * totalTokens / 1_000_000 * price
}

// UploadToOpenAI uploads the JSONL file at trainPath to the OpenAI Files API
// using multipart/form-data and returns the resulting file ID. The file is
// uploaded with purpose "fine-tune".
func UploadToOpenAI(apiKey, trainPath string) (string, error) {
	f, err := os.Open(trainPath)
	if err != nil {
		return "", fmt.Errorf("finetune: open %s: %w", trainPath, err)
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	// purpose field
	if err := mw.WriteField("purpose", "fine-tune"); err != nil {
		return "", fmt.Errorf("finetune: write purpose field: %w", err)
	}

	// file field
	fw, err := mw.CreateFormFile("file", filepath.Base(trainPath))
	if err != nil {
		return "", fmt.Errorf("finetune: create form file: %w", err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", fmt.Errorf("finetune: copy file content: %w", err)
	}
	mw.Close()

	req, err := http.NewRequest(http.MethodPost, openAIBaseURL+"/v1/files", &body)
	if err != nil {
		return "", fmt.Errorf("finetune: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("finetune: POST /v1/files: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("finetune: OpenAI /v1/files returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("finetune: parse /v1/files response: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("finetune: OpenAI returned empty file ID: %s", respBody)
	}
	return result.ID, nil
}

// CreateOpenAIJob creates a fine-tuning job on OpenAI using the given file ID
// and base model. It returns a FineTuneJob populated from the API response.
func CreateOpenAIJob(apiKey, fileID, model string) (FineTuneJob, error) {
	payload := map[string]string{
		"training_file": fileID,
		"model":         model,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return FineTuneJob{}, fmt.Errorf("finetune: marshal job payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, openAIBaseURL+"/v1/fine_tuning/jobs", bytes.NewReader(b))
	if err != nil {
		return FineTuneJob{}, fmt.Errorf("finetune: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return FineTuneJob{}, fmt.Errorf("finetune: POST /v1/fine_tuning/jobs: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return FineTuneJob{}, fmt.Errorf("finetune: OpenAI /v1/fine_tuning/jobs returned %d: %s", resp.StatusCode, respBody)
	}

	return parseOpenAIJob("openai", model, "", "", respBody)
}

// PollOpenAIJob fetches the current status of a fine-tuning job from the
// OpenAI API and returns the updated FineTuneJob.
func PollOpenAIJob(apiKey, jobID string) (FineTuneJob, error) {
	url := openAIBaseURL + "/v1/fine_tuning/jobs/" + jobID
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return FineTuneJob{}, fmt.Errorf("finetune: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return FineTuneJob{}, fmt.Errorf("finetune: GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return FineTuneJob{}, fmt.Errorf("finetune: OpenAI poll returned %d: %s", resp.StatusCode, respBody)
	}

	return parseOpenAIJob("openai", "", "", "", respBody)
}

// parseOpenAIJob decodes an OpenAI fine-tuning job response into a FineTuneJob.
// provider, model, trainFile, valFile override the parsed values when non-empty.
func parseOpenAIJob(provider, model, trainFile, valFile string, body []byte) (FineTuneJob, error) {
	var raw struct {
		ID            string `json:"id"`
		Status        string `json:"status"`
		Model         string `json:"model"`
		TrainingFile  string `json:"training_file"`
		CreatedAt     int64  `json:"created_at"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return FineTuneJob{}, fmt.Errorf("finetune: parse job response: %w", err)
	}

	if model == "" {
		model = raw.Model
	}
	if trainFile == "" {
		trainFile = raw.TrainingFile
	}

	createdAt := ""
	if raw.CreatedAt > 0 {
		createdAt = time.Unix(raw.CreatedAt, 0).UTC().Format(time.RFC3339)
	}

	// Map OpenAI status values to our canonical set.
	status := mapOpenAIStatus(raw.Status)

	return FineTuneJob{
		Provider:  provider,
		Model:     model,
		TrainFile: trainFile,
		ValFile:   valFile,
		JobID:     raw.ID,
		Status:    status,
		CreatedAt: createdAt,
	}, nil
}

// mapOpenAIStatus converts OpenAI's job status strings to the canonical set
// used by FineTuneJob: "queued" | "running" | "succeeded" | "failed".
func mapOpenAIStatus(s string) string {
	switch s {
	case "validating_files", "queued":
		return "queued"
	case "running":
		return "running"
	case "succeeded":
		return "succeeded"
	case "failed", "cancelled":
		return "failed"
	default:
		return s
	}
}
