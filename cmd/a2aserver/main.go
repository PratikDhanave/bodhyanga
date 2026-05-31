// Command a2aserver starts an A2A (Agent-to-Agent) server that exposes a
// simple "echo" agent via the Google A2A protocol.
//
// Usage:
//
//	a2aserver [-addr :8099] [-verbose]
//
// The server exposes:
//
//	GET  /.well-known/agent.json  — agent card
//	POST /tasks                    — submit a new task
//	GET  /tasks/{id}               — get task status
//	GET  /tasks/{id}/stream        — SSE stream of task updates
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/PratikDhanave/bodh/pkg/a2a"
)

func main() {
	addr := flag.String("addr", ":8099", "listen address")
	verbose := flag.Bool("verbose", false, "verbose logging")
	flag.Parse()

	card := a2a.AgentCard{
		Name:        "bodh-echo",
		Description: "A simple echo agent that reflects input messages back to the caller.",
		Version:     "0.1.0",
		URL:         "http://localhost" + *addr,
		Capabilities: a2a.Capabilities{
			Streaming:  true,
			PushNotify: false,
		},
		Skills: []a2a.Skill{
			{
				ID:          "echo",
				Name:        "Echo",
				Description: "Echoes the value of the 'message' input field back as output.",
				Tags:        []string{"echo", "test"},
			},
		},
	}

	handler := func(ctx context.Context, input map[string]any) (string, error) {
		msg, _ := input["message"].(string)
		if *verbose {
			log.Printf("[a2aserver] handling echo request: message=%q", msg)
		}
		return "Echo: " + msg, nil
	}

	cfg := a2a.Config{
		Card:    card,
		Handler: handler,
		Addr:    *addr,
	}

	srv := a2a.NewServer(cfg)

	// Print startup banner.
	fmt.Printf("A2A server starting on %s\n", *addr)
	fmt.Printf("\n  Agent card:  http://localhost%s/.well-known/agent.json\n", *addr)
	fmt.Printf("\n  Submit task example:\n")
	fmt.Printf("    curl -X POST http://localhost%s/tasks \\\n", *addr)
	fmt.Printf("      -H 'Content-Type: application/json' \\\n")
	fmt.Printf("      -d '{\"input\":{\"message\":\"hello world\"}}'\n\n")

	// Handle OS signals for graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("a2aserver: %v", err)
		}
	}()

	<-stop
	fmt.Println("\nShutting down...")
	if err := srv.Shutdown(context.Background()); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
