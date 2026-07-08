// Command caredemo exercises the care-services flows in-process:
// CDM care planning, TCM after discharge, Panel gap-detection, Virtual Office
// (inbox / refills / prior auths), and the readmission tracker.
//
// All agents run on the same MARA platform as cmd/demo; the only difference is
// the message types we publish.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/engagement"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
	"github.com/PratikDhanave/bodh/pkg/registry"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

func main() {
	workflow := flag.String("workflow", "all", "cdm|tcm|panel|virtual_office|adt|all")
	hitlOn := flag.Bool("hitl", true, "route clinically consequential actions through human_review")
	flag.Parse()

	logger := observability.NewStdLogger()
	env := &orchestration.SimpleEnvironment{Logger: logger, Clock: observability.SystemClock{}}
	reg := registry.NewInMemory()
	bus := comm.NewInMemoryBus()
	policy := governance.NewComposite(
		governance.MaxContentLengthPolicy{Max: 65536},
		governance.RequireCaseIDPolicy{},
		governance.MaxDiagnosticCostPolicy{},
		governance.ResearchDisclaimerPolicy{},
	)
	patients := seedPatients()
	queue := tasks.NewQueue()
	notifier := engagement.NewInMemory()
	reviews := hitl.NewQueue()
	reviewAgent := &agents.HumanReviewAgent{Queue: reviews}

	ctx := context.Background()
	for _, a := range []agent.Agent{
		&agents.CDMPlannerAgent{Registry: patients, HITL: *hitlOn},
		&agents.TCMCoordinatorAgent{Tasks: queue, Registry: patients, HITL: *hitlOn},
		&agents.PanelManagerAgent{Registry: patients, HITL: *hitlOn},
		&agents.PreVisitPrepAgent{Registry: patients},
		&agents.InboxTriageAgent{Tasks: queue},
		&agents.RefillManagerAgent{Tasks: queue, Registry: patients, HITL: *hitlOn},
		&agents.PriorAuthAgent{Tasks: queue, HITL: *hitlOn},
		&agents.EngagementAgent{Notifier: notifier},
		&agents.ReadmissionTrackerAgent{Registry: patients, HITL: *hitlOn},
		reviewAgent,
	} {
		_ = reg.Register(ctx, a)
	}
	orchestration.NewOrchestrator(reg, bus, policy, env).Start(ctx)

	switch *workflow {
	case "cdm":
		runCDM(ctx, bus)
	case "tcm":
		runTCM(ctx, bus)
	case "panel":
		runPanel(ctx, bus)
	case "virtual_office":
		runVirtualOffice(ctx, bus)
	case "adt":
		runADT(ctx, bus)
	default:
		runCDM(ctx, bus)
		runTCM(ctx, bus)
		runPanel(ctx, bus)
		runVirtualOffice(ctx, bus)
		runADT(ctx, bus)
	}

	// Phase 1: let upstream agents publish review_requests.
	time.Sleep(300 * time.Millisecond)

	// Phase 2: drain the review queue. Each approval republishes the original
	// payload; some of those resumed messages will themselves emit further
	// review_requests (e.g. discharge_review approval triggers TCM, which then
	// emits tcm_welcome_review). Loop until the queue is empty.
	totalApproved := 0
	for round := 1; round <= 5; round++ {
		pending := reviews.Pending()
		if len(pending) == 0 {
			break
		}
		logger.Printf("=== phase 2 round %d: clinician review (auto-approving %d pending) ===",
			round, len(pending))
		for _, r := range pending {
			_, resume, ok := reviewAgent.ApplyDecision(r.ID, "clinician-demo", "approve", "demo auto-approval", "")
			if !ok {
				continue
			}
			for _, m := range resume {
				bus.Publish(ctx, m)
			}
			totalApproved++
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Phase 3: let any remaining resumed messages flow through engagement.
	time.Sleep(300 * time.Millisecond)
	_ = totalApproved

	fmt.Println()
	logger.Printf("=== summary ===")
	logger.Printf("nurse tasks: %d open / %d total — kinds=%v",
		len(queue.FilterOpen()), len(queue.List()), queue.CountByKind())
	logger.Printf("notifications sent: %d", len(notifier.List()))
	logger.Printf("human reviews: %d total — by status %v — by kind %v",
		len(reviews.List()), reviews.CountByStatus(), reviews.CountByKind())
}

func seedPatients() *medical.PatientRegistry {
	now := time.Now()
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{
		PatientID: "pt-001", Age: 72, Sex: "F",
		Conditions:     []string{"chf", "htn"},
		LastSystolicBP: 168, LastBPDate: now.AddDate(0, 0, -5),
		LastWeightLb: 192,
		Medications: []medical.MedicationEntry{
			{Name: "Furosemide", Dose: "40 mg", Frequency: "daily"},
			{Name: "Lisinopril", Dose: "10 mg", Frequency: "daily"},
		},
		RPMEnrolled: true,
		LastVisit:   now.AddDate(0, -2, 0),
	})
	reg.Upsert(medical.PatientRecord{
		PatientID: "pt-002", Age: 58, Sex: "M",
		Conditions: []string{"dm"},
		LastA1c:    9.4, LastA1cDate: now.AddDate(0, -7, 0),
		Medications: []medical.MedicationEntry{
			{Name: "Metformin", Dose: "500 mg", Frequency: "BID"},
		},
		LastVisit: now.AddDate(-1, -2, 0),
	})
	reg.Upsert(medical.PatientRecord{
		PatientID: "pt-003", Age: 66, Sex: "M",
		Conditions: []string{"copd"},
		LastSpO2:   90,
		Medications: []medical.MedicationEntry{
			{Name: "Tiotropium", Dose: "18 mcg", Frequency: "daily"},
		},
		LastVisit: now.AddDate(0, -1, 0),
	})
	return reg
}

func runCDM(ctx context.Context, bus comm.Bus) {
	body, _ := json.Marshal(map[string]string{
		"patient_id": "pt-002", "condition": "dm", "case_id": "cdm-001",
	})
	bus.Publish(ctx, agent.NewMessage("user", "cdm_planner", agent.RoleUser,
		"cdm_request", string(body), map[string]any{"case_id": "cdm-001", "patient_id": "pt-002"}))
}

func runTCM(ctx context.Context, bus comm.Bus) {
	d := medical.DischargeSummary{
		CaseID: "tcm-001", PatientID: "pt-001",
		DischargedAt: time.Now().AddDate(0, 0, -1),
		PrimaryDx:    "Acute decompensated heart failure",
		Disposition:  "home",
		HomeMeds:     []medical.MedicationEntry{{Name: "Lisinopril", Class: "ACEI"}},
		NewMeds:      []medical.MedicationEntry{{Name: "Furosemide", Class: "Loop diuretic"}},
		HighRisk:     true,
	}
	bus.Publish(ctx, agent.NewMessage("user", "tcm_coordinator", agent.RoleUser,
		"discharge_event", medical.EncodeDischarge(d),
		map[string]any{"case_id": "tcm-001", "patient_id": d.PatientID}))
}

func runPanel(ctx context.Context, bus comm.Bus) {
	bus.Publish(ctx, agent.NewMessage("user", "panel_manager", agent.RoleUser,
		"panel_request", `{"condition":"all"}`, map[string]any{}))
}

func runVirtualOffice(ctx context.Context, bus comm.Bus) {
	// Pre-visit prep
	bus.Publish(ctx, agent.NewMessage("user", "pre_visit_prep", agent.RoleUser,
		"pre_visit_request", `{"patient_id":"pt-001","encounter_id":"enc-001"}`,
		map[string]any{"patient_id": "pt-001"}))
	// Inbox: refill request
	bus.Publish(ctx, agent.NewMessage("user", "inbox_triage", agent.RoleUser,
		"inbox_message",
		`{"patient_id":"pt-002","subject":"Refill needed","body":"I need a metformin refill please."}`,
		map[string]any{"patient_id": "pt-002"}))
	// Inbox: urgent
	bus.Publish(ctx, agent.NewMessage("user", "inbox_triage", agent.RoleUser,
		"inbox_message",
		`{"patient_id":"pt-001","subject":"Help","body":"I have chest pain right now."}`,
		map[string]any{"patient_id": "pt-001"}))
	// Prior auth
	bus.Publish(ctx, agent.NewMessage("user", "prior_auth", agent.RoleUser,
		"prior_auth_request",
		`{"patient_id":"pt-003","code":"MRI","payer":"BlueShield"}`,
		map[string]any{"patient_id": "pt-003"}))
}

func runADT(ctx context.Context, bus comm.Bus) {
	e := medical.ADTEvent{
		EventID:    "adt-001",
		PatientID:  "pt-001",
		Type:       medical.ADTDischarge,
		OccurredAt: time.Now(),
		PrimaryDx:  "CHF exacerbation",
	}
	bus.Publish(ctx, agent.NewMessage("user", "readmission_tracker", agent.RoleUser,
		"adt_event", medical.EncodeADT(e), map[string]any{"patient_id": e.PatientID}))
}
