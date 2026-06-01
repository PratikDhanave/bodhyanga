// Command finetune provides a CLI for the fine-tuning data pipeline:
// validate, split, estimate cost, upload, and poll status.
//
// It complements cmd/export-hitl-feedback: export first produces SFT/DPO JSONL
// from HITL decisions; finetune then validates and stages that data for a
// fine-tuning run on OpenAI (and eventually Anthropic).
//
// Usage:
//
//	finetune validate <dataset.jsonl>
//	finetune split    <in.jsonl> <out-dir>
//	finetune estimate <dataset.jsonl>
//	finetune upload   <dataset.jsonl>
//	finetune status   <job-id>
//
// Flags (apply globally):
//
//	-provider   "openai" | "anthropic"   (default "openai")
//	-model      base model to fine-tune   (default "gpt-4o-mini")
//	-api-key    API key (overrides env var)
//	-epochs     number of training epochs (default 3)
//	-val-ratio  fraction of data for validation (default 0.1)
//	-verbose    print extra detail
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/finetune"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("finetune", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, usage)
	}

	provider := fs.String("provider", "openai", `provider: "openai" | "anthropic"`)
	model := fs.String("model", "gpt-4o-mini", "base model to fine-tune")
	apiKey := fs.String("api-key", "", "API key (falls back to OPENAI_API_KEY / ANTHROPIC_API_KEY env vars)")
	epochs := fs.Int("epochs", 3, "number of training epochs for cost estimate")
	valRatio := fs.Float64("val-ratio", 0.1, "fraction of data for validation split")
	verbose := fs.Bool("verbose", false, "print extra detail")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}

	subCmd := remaining[0]
	subArgs := remaining[1:]

	// Resolve API key from flag or env.
	key := *apiKey
	if key == "" {
		switch strings.ToLower(*provider) {
		case "anthropic":
			key = os.Getenv("ANTHROPIC_API_KEY")
		default:
			key = os.Getenv("OPENAI_API_KEY")
		}
	}

	switch subCmd {
	case "validate":
		return cmdValidate(subArgs, *verbose)
	case "split":
		return cmdSplit(subArgs, *valRatio, *verbose)
	case "estimate":
		return cmdEstimate(subArgs, *provider, *model, *epochs, *verbose)
	case "upload":
		return cmdUpload(subArgs, *provider, key, *valRatio, *verbose)
	case "status":
		return cmdStatus(subArgs, *provider, key, *verbose)
	default:
		fmt.Fprintf(os.Stderr, "finetune: unknown subcommand %q\n\n%s\n", subCmd, usage)
		return 2
	}
}

// cmdValidate runs ValidateDataset and prints the report.
func cmdValidate(args []string, verbose bool) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: finetune validate <dataset.jsonl>")
		return 2
	}
	path := args[0]

	stats, errs, err := finetune.ValidateDataset(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "finetune validate: %v\n", err)
		return 1
	}

	fmt.Printf("Dataset: %s\n", path)
	fmt.Printf("  Total lines:          %d\n", stats.Total)
	fmt.Printf("  Avg prompt length:    %d chars\n", stats.AvgPromptLen)
	fmt.Printf("  Avg completion len:   %d chars\n", stats.AvgCompletionLen)
	if stats.MinScore != 0 || stats.MaxScore != 0 {
		fmt.Printf("  Score range:          %.4f – %.4f\n", stats.MinScore, stats.MaxScore)
	}

	if len(errs) == 0 {
		fmt.Println("  Validation: PASSED (no errors)")
		return 0
	}

	fmt.Printf("  Validation: FAILED (%d errors)\n", len(errs))
	if verbose {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "    ERROR: %s\n", e)
		}
	} else {
		limit := 5
		if len(errs) < limit {
			limit = len(errs)
		}
		for _, e := range errs[:limit] {
			fmt.Fprintf(os.Stderr, "    ERROR: %s\n", e)
		}
		if len(errs) > 5 {
			fmt.Fprintf(os.Stderr, "    ... and %d more (use -verbose to see all)\n", len(errs)-5)
		}
	}
	return 1
}

// cmdSplit shuffles and splits the dataset into train/val.
func cmdSplit(args []string, valRatio float64, verbose bool) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: finetune split <in.jsonl> <out-dir>")
		return 2
	}
	inPath := args[0]
	outDir := args[1]

	trainPath := filepath.Join(outDir, "train.jsonl")
	valPath := filepath.Join(outDir, "val.jsonl")

	if err := finetune.SplitDataset(inPath, trainPath, valPath, valRatio); err != nil {
		fmt.Fprintf(os.Stderr, "finetune split: %v\n", err)
		return 1
	}

	fmt.Printf("Split complete (val-ratio=%.2f):\n", valRatio)
	fmt.Printf("  Train: %s\n", trainPath)
	fmt.Printf("  Val:   %s\n", valPath)
	if verbose {
		printFileLineCount(trainPath, "train")
		printFileLineCount(valPath, "val")
	}
	return 0
}

// cmdEstimate prints a rough cost estimate for fine-tuning the dataset.
func cmdEstimate(args []string, provider, model string, epochs int, verbose bool) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: finetune estimate <dataset.jsonl>")
		return 2
	}
	path := args[0]

	stats, errs, err := finetune.ValidateDataset(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "finetune estimate: %v\n", err)
		return 1
	}
	if len(errs) > 0 && verbose {
		fmt.Fprintf(os.Stderr, "  warning: %d validation errors in dataset\n", len(errs))
	}

	cost := finetune.EstimateCost(stats, provider, model, epochs)
	fmt.Printf("Estimated fine-tuning cost:\n")
	fmt.Printf("  Provider:   %s\n", provider)
	fmt.Printf("  Model:      %s\n", model)
	fmt.Printf("  Examples:   %d\n", stats.Total)
	fmt.Printf("  Epochs:     %d\n", epochs)
	fmt.Printf("  Est. cost:  $%.4f USD\n", cost)
	fmt.Println("  (Estimate only — actual billing may differ.)")
	return 0
}

// cmdUpload validates, optionally splits, and uploads the dataset to the provider.
func cmdUpload(args []string, provider, apiKey string, valRatio float64, verbose bool) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: finetune upload <dataset.jsonl>")
		return 2
	}
	path := args[0]

	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "finetune upload: API key required (-api-key or env var)")
		return 2
	}

	// Validate first.
	stats, errs, err := finetune.ValidateDataset(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "finetune upload (validate): %v\n", err)
		return 1
	}
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "finetune upload: dataset has %d validation errors — fix before uploading\n", len(errs))
		for i, e := range errs {
			if i >= 5 {
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(errs)-5)
				break
			}
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
		return 1
	}

	fmt.Printf("Dataset validated: %d examples\n", stats.Total)

	switch strings.ToLower(provider) {
	case "openai":
		fileID, err := finetune.UploadToOpenAI(apiKey, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "finetune upload: %v\n", err)
			return 1
		}
		fmt.Printf("Uploaded to OpenAI — file ID: %s\n", fileID)
		fmt.Printf("Run 'finetune upload' completed. Use 'finetune status' with a job ID after creating a fine-tune job.\n")
	case "anthropic":
		fmt.Fprintln(os.Stderr, "finetune upload: Anthropic fine-tuning upload not yet supported")
		return 1
	default:
		fmt.Fprintf(os.Stderr, "finetune upload: unknown provider %q\n", provider)
		return 2
	}
	return 0
}

// cmdStatus polls the status of a fine-tuning job.
func cmdStatus(args []string, provider, apiKey string, verbose bool) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: finetune status <job-id>")
		return 2
	}
	jobID := args[0]

	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "finetune status: API key required (-api-key or env var)")
		return 2
	}

	switch strings.ToLower(provider) {
	case "openai":
		job, err := finetune.PollOpenAIJob(apiKey, jobID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "finetune status: %v\n", err)
			return 1
		}
		fmt.Printf("Job ID:     %s\n", job.JobID)
		fmt.Printf("Status:     %s\n", job.Status)
		fmt.Printf("Model:      %s\n", job.Model)
		fmt.Printf("Created:    %s\n", job.CreatedAt)
		if verbose && job.TrainFile != "" {
			fmt.Printf("Train file: %s\n", job.TrainFile)
		}
	case "anthropic":
		fmt.Fprintln(os.Stderr, "finetune status: Anthropic job status not yet supported")
		return 1
	default:
		fmt.Fprintf(os.Stderr, "finetune status: unknown provider %q\n", provider)
		return 2
	}
	return 0
}

// printFileLineCount is a verbose helper that counts non-empty lines in a file.
func printFileLineCount(path, label string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	count := 0
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			count++
		}
	}
	fmt.Printf("  %s lines: %d\n", label, count)
}

const usage = `finetune — fine-tuning data pipeline for Bodh

Subcommands:
  validate <dataset.jsonl>    validate SFT JSONL and print stats
  split    <in.jsonl> <dir>   shuffle and split into train/val
  estimate <dataset.jsonl>    estimate fine-tuning cost
  upload   <dataset.jsonl>    upload to OpenAI (or Anthropic)
  status   <job-id>           poll fine-tuning job status

Flags:
  -provider   openai | anthropic (default "openai")
  -model      base model         (default "gpt-4o-mini")
  -api-key    API key            (env: OPENAI_API_KEY / ANTHROPIC_API_KEY)
  -epochs     training epochs    (default 3, used by estimate)
  -val-ratio  validation ratio   (default 0.1, used by split/upload)
  -verbose    print extra detail`
