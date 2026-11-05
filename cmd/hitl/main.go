// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Command hitl demonstrates Lesson 09: Human-in-the-Loop approval gates.
//
// # What it teaches
//
// Before an agent takes a consequential action — in this lesson, sending an
// email — the agent pauses and asks a human to approve or reject the draft.
// This is the core Human-in-the-Loop (HITL) pattern:
//
//  1. Agent calls the draft_email tool.
//  2. The tool submits the draft to a pkg/hitl.Queue and returns a pending ID.
//  3. The CLI reads the queue, displays the draft, and prompts the user Y/N.
//  4. The decision (approve / reject + reason) is fed back via Queue.Decide.
//  5. The agent reads the decision and either "sends" the email or revises.
//
// # Scenario
//
// The user asks: "Help me draft a follow-up email to Alice about our meeting."
// The agent drafts the email, gates on human approval, and keeps revising
// until the user approves or the maximum retry limit is reached.
//
// # Usage
//
//	go run ./cmd/hitl/...
//	go run ./cmd/hitl/... -provider anthropic
//	go run ./cmd/hitl/... -message "Draft a thank-you email to Bob" -verbose
//
// # Flags
//
//	-provider     LLM provider: "ollama" (default) or "anthropic"
//	-model        Model override (defaults to provider default)
//	-ollama-host  Ollama base URL (default: http://localhost:11434)
//	-message      Initial user request (default: follow-up email to Alice)
//	-verbose      Print raw LLM request/response details
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/PratikDhanave/bodhyanga/pkg/hitl"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// maxRevisions is the maximum number of draft-revise cycles before giving up.
// In production this would be configurable; three rounds is enough for the demo.
const maxRevisions = 3

func main() {
	providerFlag := flag.String("provider",
		envDefault("HITL_PROVIDER", "ollama"),
		`LLM provider: "ollama" (default, no API key needed) or "anthropic"`)
	modelFlag := flag.String("model",
		envDefault("HITL_MODEL", ""),
		"Model override (defaults to provider default)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	messageFlag := flag.String("message",
		envDefault("HITL_MESSAGE", "Help me draft a follow-up email to Alice about our meeting"),
		"Initial user request")
	verboseFlag := flag.Bool("verbose", false, "Print raw LLM request/response details")
	flag.Parse()

	// Resolve model default.
	model := *modelFlag
	if model == "" {
		switch *providerFlag {
		case "anthropic":
			model = "claude-haiku-4-5"
		default: // ollama
			model = "llama3.2"
		}
	}

	// Build LLM client.
	client, err := buildClient(*providerFlag, model, *ollamaHostFlag)
	if err != nil {
		log.Fatalf("hitl: build client: %v", err)
	}

	fmt.Printf("hitl: provider=%s  model=%s\n", *providerFlag, model)
	fmt.Printf("hitl: request: %q\n\n", *messageFlag)

	// Run the HITL demo loop.
	if err := run(context.Background(), client, *messageFlag, *verboseFlag); err != nil {
		log.Fatalf("hitl: %v", err)
	}
}

// run is the main demo loop. It drives the agent through draft → review → revise
// cycles until the human approves or the revision limit is reached.
func run(ctx context.Context, client llm.Client, userMessage string, verbose bool) error {
	// The HITL queue is the coordination point between the agent and the human
	// reviewer. In a real system this would be backed by Postgres; here we use
	// the in-memory queue from pkg/hitl.
	queue := hitl.NewQueue()

	// stdin scanner for interactive prompts.
	scanner := bufio.NewScanner(os.Stdin)

	// conversationMessages holds the full turn history for the agent's context
	// window. We grow it turn by turn so the model can see its own prior
	// drafts and the rejection reasons.
	var conversationMessages []llm.Message

	// tools advertised to the LLM: one tool, draft_email, which the model
	// calls to produce a draft and gate on approval.
	tools := []llm.Tool{
		{
			Name:        "draft_email",
			Description: "Draft an email to be reviewed by a human before sending. Returns a review ID that resolves to 'approved' or 'rejected' with a reason. Call this tool with the draft email; the human will approve or request changes.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"to":      {"type": "string", "description": "Recipient name or email address"},
					"subject": {"type": "string", "description": "Email subject line"},
					"body":    {"type": "string", "description": "Full email body text"}
				},
				"required": ["to", "subject", "body"]
			}`),
		},
	}

	// System prompt explains the agent's role and the approval-gate workflow.
	system := `You are a professional email assistant. Your job is to help the user
draft and send emails. When drafting an email:
1. Call the draft_email tool with a well-written draft.
2. Wait for the human approval result.
3. If approved: report that the email was sent.
4. If rejected: read the rejection reason carefully and call draft_email again
   with an improved version that addresses the feedback.
Never "send" an email without calling draft_email first. The tool output tells
you whether the draft was approved or rejected.`

	// Seed the conversation with the user's request.
	conversationMessages = append(conversationMessages, llm.Message{
		Role:    llm.RoleUser,
		Content: userMessage,
	})

	for revision := 0; revision < maxRevisions; revision++ {
		if revision > 0 {
			fmt.Printf("\n--- revision %d of %d ---\n", revision+1, maxRevisions)
		}

		// Build the LLM request for this round.
		req := llm.Request{
			System:    system,
			Messages:  conversationMessages,
			Tools:     tools,
			MaxTokens: 1024,
		}

		if verbose {
			fmt.Printf("[verbose] LLM request: %d messages, %d tools\n", len(req.Messages), len(req.Tools))
		}

		// First LLM call: ask the model to draft the email.
		resp, err := client.Complete(ctx, req)
		if err != nil {
			return fmt.Errorf("llm complete (revision %d): %w", revision+1, err)
		}

		if verbose {
			fmt.Printf("[verbose] LLM response: stop=%s tool_calls=%d content=%q\n",
				resp.StopReason, len(resp.ToolCalls), resp.Content)
		}

		// If the model did not call draft_email, it may have produced a text
		// response. Print it and exit — the demo is done.
		if len(resp.ToolCalls) == 0 {
			if resp.Content != "" {
				fmt.Printf("\nAgent: %s\n", resp.Content)
			}
			return nil
		}

		// Record the assistant's tool-call turn.
		conversationMessages = append(conversationMessages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Process each tool call. In practice the model calls draft_email once
		// per round, but we handle multiple calls for robustness.
		var toolResults []llm.ToolResult

		for _, call := range resp.ToolCalls {
			if call.Name != "draft_email" {
				// Unknown tool — return a stub error so the model can recover.
				toolResults = append(toolResults, llm.ToolResult{
					ToolUseID: call.ID,
					Content:   `{"error":"unknown tool"}`,
				})
				continue
			}

			// Parse the draft from the model's arguments.
			draft, parseErr := parseDraftArgs(call.Arguments)
			if parseErr != nil {
				toolResults = append(toolResults, llm.ToolResult{
					ToolUseID: call.ID,
					Content:   fmt.Sprintf(`{"error":%q}`, parseErr.Error()),
				})
				continue
			}

			// Submit the draft to the HITL queue. The Kind "email_draft" is
			// non-clinical; the Subject / Detail fields carry the human-readable
			// summary shown to the reviewer.
			req := medical.ReviewRequest{
				Kind:        "email_draft",
				Priority:    "normal",
				Subject:     fmt.Sprintf("Email to %s: %s", draft.To, draft.Subject),
				Detail:      draft.Body,
				NextTo:      "email_sender",
				NextType:    "send_email",
				Payload:     string(call.Arguments),
				SubmittedBy: "hitl-demo-agent",
			}
			submitted := queue.Submit(req)

			fmt.Printf("\n=== DRAFT EMAIL (review ID: %s) ===\n", submitted.ID)
			fmt.Printf("To:      %s\n", draft.To)
			fmt.Printf("Subject: %s\n", draft.Subject)
			fmt.Printf("---\n%s\n---\n", draft.Body)

			// Prompt the human for a decision.
			decision, reason := promptHuman(scanner)

			// Record the decision in the queue.
			_, ok := queue.Decide(submitted.ID, "human-reviewer", decision, reason, "")
			if !ok {
				return fmt.Errorf("queue.Decide failed for id=%s", submitted.ID)
			}

			// Build the tool result JSON to feed back to the model.
			var toolResult string
			switch decision {
			case "approve":
				// On approval: "send" the email (print confirmation) and inform
				// the model.
				fmt.Printf("\n[HITL] Approved. Sending email to %s...\n", draft.To)
				fmt.Printf("[SENT] To: %s | Subject: %s\n\n", draft.To, draft.Subject)
				toolResult = `{"status":"approved","message":"Email sent successfully."}`

			case "reject":
				fmt.Printf("\n[HITL] Rejected. Reason: %s\n", reason)
				toolResult = fmt.Sprintf(`{"status":"rejected","reason":%q}`, reason)

			default:
				toolResult = fmt.Sprintf(`{"status":"rejected","reason":%q}`, reason)
			}

			toolResults = append(toolResults, llm.ToolResult{
				ToolUseID: call.ID,
				Content:   toolResult,
			})

			// If the email was approved we are done — no more revisions needed.
			if decision == "approve" {
				// Append the tool results turn so conversation history is
				// complete, then ask the model for a final confirmation message.
				conversationMessages = append(conversationMessages, llm.Message{
					Role:        llm.RoleUser,
					ToolResults: toolResults,
				})
				return finalConfirmation(ctx, client, system, conversationMessages, verbose)
			}
		}

		// The draft was rejected (or had errors). Append the tool results and
		// let the loop continue to the next revision.
		conversationMessages = append(conversationMessages, llm.Message{
			Role:        llm.RoleUser,
			ToolResults: toolResults,
		})
	}

	fmt.Printf("\n[hitl] Maximum revisions (%d) reached without approval. Exiting.\n", maxRevisions)
	return nil
}

// finalConfirmation makes one last LLM call to get a human-readable
// "email sent" confirmation message from the agent, then prints it.
func finalConfirmation(ctx context.Context, client llm.Client, system string, messages []llm.Message, verbose bool) error {
	req := llm.Request{
		System:    system,
		Messages:  messages,
		MaxTokens: 256,
	}
	resp, err := client.Complete(ctx, req)
	if err != nil {
		// Non-fatal: the email was already "sent", just skip the confirmation.
		if verbose {
			fmt.Printf("[verbose] finalConfirmation LLM error (non-fatal): %v\n", err)
		}
		return nil
	}
	if resp.Content != "" {
		fmt.Printf("Agent: %s\n", resp.Content)
	}
	return nil
}

// draftArgs is the JSON structure the model sends to the draft_email tool.
type draftArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// parseDraftArgs decodes the model's tool call arguments.
func parseDraftArgs(raw json.RawMessage) (draftArgs, error) {
	var d draftArgs
	if err := json.Unmarshal(raw, &d); err != nil {
		return draftArgs{}, fmt.Errorf("parse draft_email args: %w", err)
	}
	if d.To == "" || d.Subject == "" || d.Body == "" {
		return draftArgs{}, fmt.Errorf("draft_email requires to, subject, and body")
	}
	return d, nil
}

// promptHuman displays the approval prompt and reads the user's decision from
// stdin. Returns ("approve", "") or ("reject", reason).
func promptHuman(scanner *bufio.Scanner) (decision, reason string) {
	for {
		fmt.Print("\nApprove this draft? [Y/n]: ")
		if !scanner.Scan() {
			// EOF or scanner error — treat as rejection.
			return "reject", "no input (EOF)"
		}
		answer := strings.TrimSpace(scanner.Text())
		switch strings.ToLower(answer) {
		case "", "y", "yes":
			return "approve", ""
		case "n", "no":
			fmt.Print("Rejection reason (what should be changed?): ")
			if !scanner.Scan() {
				return "reject", "no reason given"
			}
			reason = strings.TrimSpace(scanner.Text())
			if reason == "" {
				reason = "no reason given"
			}
			return "reject", reason
		default:
			fmt.Printf("Please enter Y or N.\n")
		}
	}
}

// buildClient constructs an llm.Client for the given provider.
func buildClient(provider, model, ollamaHost string) (llm.Client, error) {
	switch provider {
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY must be set when provider=anthropic")
		}
		return llm.NewAnthropic(apiKey, model), nil
	case "ollama":
		return llm.NewOllama(ollamaHost, model), nil
	default:
		return nil, fmt.Errorf("unknown provider %q — use \"ollama\" or \"anthropic\"", provider)
	}
}

// envDefault returns the environment variable value or the fallback string.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

