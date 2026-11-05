// Command federated-eval generates mock site reports, aggregates them into a
// federated evaluation report, and optionally writes the result to a JSON
// file. It also starts a minimal HTTP server with POST /api/federated/report
// and GET /api/federated/aggregate endpoints when the -serve flag is set.
//
// Usage:
//
//	go run ./cmd/federated-eval -sites 5 -epsilon 0 -output report.json
//	go run ./cmd/federated-eval -sites 10 -epsilon 1.0 -serve :8099
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/eval/federated"
)

func main() {
	sites := flag.Int("sites", 5, "number of mock sites to generate")
	epsilon := flag.Float64("epsilon", 0, "differential-privacy epsilon (0 = disabled)")
	output := flag.String("output", "", "path to write the aggregated JSON report (empty = stdout)")
	serve := flag.String("serve", "", "start HTTP server on this address (e.g. :8099)")
	flag.Parse()

	coord := federated.NewFederatedCoordinator(*epsilon)

	// ── Generate mock site reports ──────────────────────────────────────
	conditions := []string{"diabetes", "hypertension", "asthma", "sepsis", "pneumonia"}
	for i := 0; i < *sites; i++ {
		cases := 50 + rand.Intn(451) // 50–500
		acc := 0.60 + rand.Float64()*0.35
		byCond := make(map[string]float64, len(conditions))
		for _, c := range conditions {
			byCond[c] = 0.55 + rand.Float64()*0.40
		}
		coord.SubmitSiteReport(federated.SiteReport{
			SiteID:       fmt.Sprintf("site-%03d", i+1),
			SiteName:     fmt.Sprintf("Hospital %c", rune('A'+i%26)),
			TotalCases:   cases,
			Accuracy:     acc,
			ByCondition:  byCond,
			AvgLatencyMs: 50 + rand.Float64()*200,
			AvgCostUSD:   0.01 + rand.Float64()*0.10,
			ReportedAt:   time.Now().UTC(),
		})
	}

	// ── Aggregate & print ───────────────────────────────────────────────
	report := coord.Aggregate()
	federated.PrintReport(report)

	// ── Export ───────────────────────────────────────────────────────────
	if *output != "" {
		if err := coord.ExportReport(*output); err != nil {
			fmt.Fprintf(os.Stderr, "export: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\nReport written to %s\n", *output)
	}

	// ── Optional HTTP server ────────────────────────────────────────────
	if *serve != "" {
		mux := http.NewServeMux()
		wireHTTP(mux, coord)
		fmt.Printf("\nServing federated eval API on %s\n", *serve)
		if err := http.ListenAndServe(*serve, mux); err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
			os.Exit(1)
		}
	}
}

// wireHTTP registers the federated eval endpoints on the given mux.
func wireHTTP(mux *http.ServeMux, coord *federated.FederatedCoordinator) {
	// POST /api/federated/report — submit a SiteReport.
	mux.HandleFunc("POST /api/federated/report", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var sr federated.SiteReport
		if err := json.Unmarshal(body, &sr); err != nil {
			http.Error(w, "decode: "+err.Error(), http.StatusBadRequest)
			return
		}
		coord.SubmitSiteReport(sr)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		resp, _ := json.Marshal(map[string]string{"status": "accepted", "site_id": sr.SiteID})
		w.Write(resp)
	})

	// GET /api/federated/aggregate — return the aggregated FederatedReport.
	mux.HandleFunc("GET /api/federated/aggregate", func(w http.ResponseWriter, r *http.Request) {
		data, err := federated.HandleAggregate(coord)
		if err != nil {
			http.Error(w, "aggregate: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})
}
