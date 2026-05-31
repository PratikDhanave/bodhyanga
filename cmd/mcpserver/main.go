// Command mcpserver starts a Model Context Protocol (MCP) server that exposes
// Bodh's clinical tool registry over HTTP+SSE (JSON-RPC 2.0).
//
// The server currently registers three tools:
//
//	drug_interaction  — Check a drug list for known pairwise interactions
//	fhir_query        — Stub FHIR Patient lookup (8 synthetic patients)
//	risk_calculator   — Compute a standard clinical risk score
//
// Usage:
//
//	mcpserver [-addr :8097] [-verbose]
//
// Connect any MCP-compatible client to:
//
//	GET  http://localhost:8097/sse      (SSE event stream — discovers message URL)
//	POST http://localhost:8097/message  (JSON-RPC 2.0 message endpoint)
//
// Example with curl:
//
//	# List available tools
//	curl -s -X POST http://localhost:8097/message \
//	     -H 'Content-Type: application/json' \
//	     -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
//
//	# Call drug_interaction
//	curl -s -X POST http://localhost:8097/message \
//	     -H 'Content-Type: application/json' \
//	     -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"drug_interaction","arguments":{"drugs":["warfarin","ibuprofen"]}}}'
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/PratikDhanave/bodh/pkg/mcp/server"
	"github.com/PratikDhanave/bodh/pkg/tools"
)

func main() {
	addr := flag.String("addr", ":8097", "TCP listen address (e.g. :8097 or 127.0.0.1:8097)")
	verbose := flag.Bool("verbose", false, "print request/response logs to stderr")
	flag.Parse()

	// Build the tool registry with all clinical tools.
	reg := tools.NewRegistry(
		tools.DrugInteraction{},
		tools.FHIRQuery{},
		tools.DefaultRiskCalculator(),
	)

	cfg := server.Config{
		Addr:    *addr,
		Name:    "bodh-mcp-server",
		Version: "0.1.0",
	}
	srv := server.New(reg, cfg)

	printStartupBanner(*addr, *verbose)

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("mcpserver: %v", err)
	}
}

// printStartupBanner writes connection instructions to stdout.
func printStartupBanner(addr string, verbose bool) {
	host := addr
	// If addr starts with ":", prepend "localhost" for the printed URL.
	if len(addr) > 0 && addr[0] == ':' {
		host = "localhost" + addr
	}

	fmt.Fprintf(os.Stdout, "Bodh MCP Server\n")
	fmt.Fprintf(os.Stdout, "───────────────────────────────────────────────────────\n")
	fmt.Fprintf(os.Stdout, "Listening on %s\n\n", addr)
	fmt.Fprintf(os.Stdout, "MCP endpoints:\n")
	fmt.Fprintf(os.Stdout, "  GET  http://%s/sse       (SSE — discovers message URL)\n", host)
	fmt.Fprintf(os.Stdout, "  POST http://%s/message   (JSON-RPC 2.0)\n\n", host)
	fmt.Fprintf(os.Stdout, "Quick test:\n")
	fmt.Fprintf(os.Stdout, "  curl -s -X POST http://%s/message \\\n", host)
	fmt.Fprintf(os.Stdout, "       -H 'Content-Type: application/json' \\\n")
	fmt.Fprintf(os.Stdout, `       -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'`)
	fmt.Fprintf(os.Stdout, "\n\n")
	fmt.Fprintf(os.Stdout, "Registered tools:\n")
	fmt.Fprintf(os.Stdout, "  drug_interaction  — pairwise drug interaction check\n")
	fmt.Fprintf(os.Stdout, "  fhir_query        — stub FHIR Patient lookup\n")
	fmt.Fprintf(os.Stdout, "  risk_calculator   — Wells PE / CURB-65 / CHA2DS2-VASc / HAS-BLED\n")
	if verbose {
		fmt.Fprintf(os.Stdout, "\n[verbose mode: request/response logging enabled]\n")
	}
	fmt.Fprintf(os.Stdout, "───────────────────────────────────────────────────────\n")
}
