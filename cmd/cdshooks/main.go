// Command cdshooks runs the Bodhyanga CDS Hooks service, which exposes four
// built-in clinical decision support hooks to EHR systems (Epic, Cerner, etc.)
// via the HL7 CDS Hooks standard (https://cds-hooks.org/).
//
// # Default address: :8096
//
// Start the service:
//
//	go run ./cmd/cdshooks/...
//
// Discover registered hooks:
//
//	curl http://localhost:8096/cds-services
//
// Fire the patient-view hook:
//
//	curl -X POST http://localhost:8096/cds-services/bodhyanga-patient-view \
//	  -H 'Content-Type: application/json' \
//	  -d '{"hook":"patient-view","hookInstance":"abc","context":{"patientId":"p1"}}'
//
// # Hooks
//
//   - patient-view       — preventive care gap alerts (warning cards)
//   - medication-prescribe — drug-drug interaction check (critical cards for HIGH severity)
//   - order-sign         — diagnostic cost vs plan budget (info card if over threshold)
//   - encounter-discharge — TCM follow-up suggestion (info card)
//
// # Environment variables
//
//	CDSHOOKS_ADDR — listen address (default :8096)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/PratikDhanave/bodhyanga/pkg/cdshooks"
)

func main() {
	addrFlag := flag.String("addr",
		envDefault("CDSHOOKS_ADDR", ":8096"),
		"HTTP listen address (e.g. :8096)")
	verboseFlag := flag.Bool("verbose", false, "Log each incoming hook request")
	flag.Parse()

	srv := cdshooks.NewServer()

	for _, pair := range cdshooks.BuiltinServices() {
		svc := pair.Service
		h := pair.Handler
		if *verboseFlag {
			h = verboseHandler(pair.Handler)
		}
		srv.Register(svc, h)
	}

	discoveryURL := fmt.Sprintf("http://localhost%s/cds-services", *addrFlag)
	if *addrFlag != "" && (*addrFlag)[0] != ':' {
		discoveryURL = fmt.Sprintf("http://%s/cds-services", *addrFlag)
	}

	fmt.Printf("Bodhyanga CDS Hooks service\n")
	fmt.Printf("  Discovery : %s\n", discoveryURL)
	fmt.Printf("  Hooks     : patient-view, medication-prescribe, order-sign, encounter-discharge\n")
	if *verboseFlag {
		fmt.Printf("  Verbose   : enabled\n")
	}
	fmt.Println()

	handler := srv.Handler()
	if *verboseFlag {
		handler = loggingMiddleware(handler)
	}

	log.Printf("cdshooks: listening on %s", *addrFlag)
	if err := http.ListenAndServe(*addrFlag, handler); err != nil {
		log.Fatalf("cdshooks: %v", err)
	}
}

// verboseHandler wraps a Handler to log hook and instance on each call.
func verboseHandler(inner cdshooks.Handler) cdshooks.Handler {
	return func(ctx context.Context, req cdshooks.CDSRequest) (cdshooks.CDSResponse, error) {
		log.Printf("cdshooks: hook=%s instance=%s", req.Hook, req.HookInstance)
		return inner(ctx, req)
	}
}

// loggingMiddleware logs method + path for every request.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("cdshooks: %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// envDefault returns the environment variable value or the fallback.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
