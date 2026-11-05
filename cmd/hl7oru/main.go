// Command hl7oru reads an HL7 v2 ORU^R01 (lab result) message from stdin and
// prints the parsed observations in a human-readable tabular format.
//
// Usage:
//
//	cat result.hl7 | hl7oru
//	hl7oru < result.hl7
//
// Output columns: Panel | LOINC | Test | Value | Units | Ref Range | Flag | Critical | Status | ObservedAt
//
// Exit codes:
//
//	0 — parsed successfully
//	1 — I/O error or parse failure
package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/hl7v2"
)

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hl7oru: read stdin: %v\n", err)
		os.Exit(1)
	}

	msg, err := hl7v2.ParseORU(string(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "hl7oru: parse: %v\n", err)
		os.Exit(1)
	}

	printSummary(msg)
	printObservations(msg)
}

func printSummary(msg *hl7v2.ORUMessage) {
	fmt.Printf("Message Control ID : %s\n", msg.MSH.ControlID)
	fmt.Printf("Sending Application: %s @ %s\n", msg.MSH.SendingApp, msg.MSH.SendingFacility)
	fmt.Printf("Patient ID         : %s (%s, %s)\n", msg.PID.PatientID, msg.PID.FamilyName, msg.PID.GivenName)
	if msg.ORC != nil {
		fmt.Printf("Order              : placer=%s filler=%s control=%s\n",
			msg.ORC.PlacerID, msg.ORC.FillerID, msg.ORC.OrderControl)
	}
	fmt.Printf("Panels             : %d\n\n", len(msg.Results))
}

func printObservations(msg *hl7v2.ORUMessage) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PANEL\tLOINC\tTEST\tVALUE\tUNITS\tREF RANGE\tFLAG\tCRITICAL\tSTATUS\tOBSERVED AT")
	fmt.Fprintln(w, "-----\t-----\t----\t-----\t-----\t---------\t----\t--------\t------\t-----------")
	for _, lr := range msg.Results {
		for _, obs := range lr.Observations {
			critical := "no"
			if obs.IsCritical {
				critical = "YES"
			}
			observedAt := ""
			if !obs.ObservedAt.IsZero() {
				observedAt = obs.ObservedAt.UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				lr.PanelName,
				obs.LOINCCode,
				obs.TestName,
				obs.Value,
				obs.Units,
				obs.ReferenceRange,
				obs.AbnormalFlag,
				critical,
				obs.Status,
				observedAt,
			)
		}
	}
	w.Flush()
}
