// Command bodhctl is the operator CLI for the cmd/care HTTP API. Stdlib-only;
// the binary stays small and the dependency surface stays clean.
//
// Set BODH_API_URL (default http://localhost:8088) to point at a running care
// server.
//
//	bodhctl reviews list                  # all reviews
//	bodhctl reviews pending               # only pending
//	bodhctl reviews approve <id> [reason] # decide
//	bodhctl reviews reject  <id> <reason>
//	bodhctl tasks list                    # nurse task queue
//	bodhctl notifications list            # outreach inbox
//	bodhctl panel <condition|all>         # trigger a panel run
//	bodhctl case run <path-to-fixture>    # submit a fixture as cdm_request
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultAPI = "http://localhost:8088"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	api := os.Getenv("BODH_API_URL")
	if api == "" {
		api = defaultAPI
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "reviews":
		reviewsCmd(api, args)
	case "tasks":
		simpleGet(api, "/tasks")
	case "notifications":
		simpleGet(api, "/notifications")
	case "panel":
		panelCmd(api, args)
	case "case":
		caseCmd(api, args)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `bodhctl — operator CLI for cmd/care

Environment:
  BODH_API_URL   target API base URL (default %s)

Subcommands:
  reviews list                     list all reviews
  reviews pending                  list pending reviews only
  reviews approve <id> [reason]    approve a review (resumes the gated message)
  reviews reject  <id> <reason>    reject a review (no resume)
  tasks list                       list the nurse work queue
  notifications list               list outreach inbox
  panel [condition|all]            run panel rules
  case run <path>                  submit a case fixture as cdm_request

`, defaultAPI)
}

func reviewsCmd(api string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "reviews: missing subcommand (list|pending|approve|reject)")
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		simpleGet(api, "/reviews")
	case "pending":
		simpleGet(api, "/reviews?status=pending")
	case "approve":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "reviews approve: missing id")
			os.Exit(2)
		}
		reason := ""
		if len(args) >= 3 {
			reason = strings.Join(args[2:], " ")
		}
		decide(api, args[1], "approve", reason)
	case "reject":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "reviews reject: missing id or reason")
			os.Exit(2)
		}
		decide(api, args[1], "reject", strings.Join(args[2:], " "))
	default:
		fmt.Fprintf(os.Stderr, "reviews: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func decide(api, id, decision, reason string) {
	body := map[string]string{
		"reviewer_id": userOrCLI(),
		"decision":    decision,
		"reason":      reason,
	}
	b, _ := json.Marshal(body)
	resp, err := postJSON(api+"/reviews/"+id+"/decide", b)
	if err != nil {
		fmt.Fprintf(os.Stderr, "decide failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(resp))
}

func panelCmd(api string, args []string) {
	condition := "all"
	if len(args) >= 1 {
		condition = args[0]
	}
	body, _ := json.Marshal(map[string]string{"condition": condition})
	resp, err := postJSON(api+"/panel", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "panel failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(resp))
}

func caseCmd(api string, args []string) {
	fs := flag.NewFlagSet("case", flag.ExitOnError)
	patient := fs.String("patient", "pt-001", "patient id (when fixture does not specify)")
	condition := fs.String("condition", "", "condition override (htn/dm/chf/ckd/copd); empty = take from fixture")
	if len(args) < 2 || args[0] != "run" {
		fmt.Fprintln(os.Stderr, "case: usage: bodhctl case run <fixture-path>")
		os.Exit(2)
	}
	_ = fs.Parse(args[2:])

	b, err := os.ReadFile(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "case: %v\n", err)
		os.Exit(1)
	}
	var fixture struct {
		CaseID    string `json:"case_id"`
		Condition string `json:"condition"`
	}
	_ = json.Unmarshal(b, &fixture)
	cond := *condition
	if cond == "" {
		cond = fixture.Condition
	}
	if cond == "" {
		cond = "dm"
	}
	caseID := fixture.CaseID
	if caseID == "" {
		caseID = fmt.Sprintf("cli-%d", time.Now().Unix())
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": *patient,
		"condition":  cond,
		"case_id":    caseID,
	})
	resp, err := postJSON(api+"/cdm", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "case run failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(resp))
}

func simpleGet(api, path string) {
	req, _ := http.NewRequest(http.MethodGet, api+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "GET %s failed: %v\n", path, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "%s -> %d: %s\n", path, resp.StatusCode, string(b))
		os.Exit(1)
	}
	prettyPrint(resp.Body)
}

func postJSON(url string, body []byte) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%d: %s", resp.StatusCode, string(b))
	}
	return b, nil
}

func prettyPrint(r io.Reader) {
	var v any
	dec := json.NewDecoder(r)
	if err := dec.Decode(&v); err != nil {
		// Not JSON — emit raw.
		_, _ = io.Copy(os.Stdout, r)
		fmt.Println()
		return
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func userOrCLI() string {
	if u := os.Getenv("USER"); u != "" {
		return "cli:" + u
	}
	return "cli:bodhctl"
}
