package main

// FHIR Bulk Data export HTTP handlers.
//
// Flow (FHIR Bulk Data Access IG §2):
//
//	POST   /fhir/$export          → 202 Accepted + Content-Location header
//	GET    /fhir/bulk/{id}        → 202 (in-progress) or 200 (complete manifest)
//	GET    /fhir/bulk/{id}/{file} → 200 NDJSON file download

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/PratikDhanave/bodhyanga/pkg/fhir"
)

// mountBulkExport registers the FHIR Bulk Data routes on mux and returns the
// BulkExporter so callers can inject data into the same registry.
func mountBulkExport(mux *http.ServeMux, exp *fhir.BulkExporter) {
	mux.HandleFunc("POST /fhir/$export", bulkKickOffHandler(exp))
	mux.HandleFunc("GET /fhir/bulk/{id}", bulkStatusHandler(exp))
	// Go 1.22+ pattern: /fhir/bulk/{id}/{file} captures the filename segment.
	mux.HandleFunc("GET /fhir/bulk/{id}/{file}", bulkDownloadHandler(exp))
}

// bulkKickOffHandler handles POST /fhir/$export.
//
// Accepts an optional JSON body with a "_type" field (comma-separated or JSON
// array) to restrict the exported resource types. Returns 202 with a
// Content-Location header pointing to the status endpoint.
//
// Example body:
//
//	{"_type": "Patient,Condition"}
func bulkKickOffHandler(exp *fhir.BulkExporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse optional _type parameter.
		var types []string
		var body struct {
			Type string   `json:"_type"`
			TypeList []string `json:"_typeList,omitempty"`
		}
		if r.ContentLength > 0 {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		if len(body.TypeList) > 0 {
			types = body.TypeList
		} else if body.Type != "" {
			for _, t := range strings.Split(body.Type, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					types = append(types, t)
				}
			}
		}
		// Also honour the FHIR query parameter form: POST /fhir/$export?_type=Patient
		if q := r.URL.Query().Get("_type"); q != "" && len(types) == 0 {
			for _, t := range strings.Split(q, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					types = append(types, t)
				}
			}
		}

		jobID := exp.StartExport(types)

		// FHIR Bulk Data IG §3.1: respond 202 with Content-Location.
		w.Header().Set("Content-Location", "/fhir/bulk/"+jobID)
		w.WriteHeader(http.StatusAccepted)
	}
}

// bulkStatusHandler handles GET /fhir/bulk/{id}.
//
// Returns 202 while the job is in-progress, or 200 with the output manifest
// JSON when complete. Returns 404 for unknown job IDs and 500 for errored jobs.
func bulkStatusHandler(exp *fhir.BulkExporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID := r.PathValue("id")
		job, ok := exp.GetStatus(jobID)
		if !ok {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}

		switch job.Status {
		case "accepted", "in-progress":
			// FHIR Bulk Data IG §3.2: return 202 with X-Progress while running.
			w.Header().Set("X-Progress", job.Status)
			w.WriteHeader(http.StatusAccepted)

		case "completed":
			// FHIR Bulk Data IG §3.3: return 200 with the output manifest.
			writeJSON(w, map[string]any{
				"transactionTime":     job.TransactionTime,
				"request":             job.Request,
				"requiresAccessToken": job.RequiresAccessToken,
				"output":              job.Output,
				"error":               job.Error,
			})

		case "error":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "error",
				"error":  job.Error,
			})
		}
	}
}

// bulkDownloadHandler handles GET /fhir/bulk/{id}/{file}.
//
// Serves the requested NDJSON file for a completed export job.
func bulkDownloadHandler(exp *fhir.BulkExporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID := r.PathValue("id")
		file := r.PathValue("file")

		data, err := exp.GetFile(jobID, file)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, err.Error(), http.StatusNotFound)
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			return
		}

		w.Header().Set("Content-Type", "application/fhir+ndjson")
		_, _ = w.Write(data)
	}
}
