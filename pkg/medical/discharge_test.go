package medical

import "testing"

func TestReconcileMedications(t *testing.T) {
	home := []MedicationEntry{
		{Name: "Lisinopril", Class: "ACEI"},
		{Name: "Atorvastatin", Class: "Statin"},
	}
	current := []MedicationEntry{
		{Name: "Lisinopril", Class: "ACEI"},
		{Name: "Furosemide", Class: "Loop diuretic"},
		{Name: "Bumetanide", Class: "Loop diuretic"},
	}
	rec := ReconcileMedications(home, current)
	if len(rec.Kept) != 1 || rec.Kept[0].Name != "Lisinopril" {
		t.Errorf("want kept=Lisinopril, got %+v", rec.Kept)
	}
	if len(rec.Stopped) != 1 || rec.Stopped[0] != "atorvastatin" {
		t.Errorf("want stopped=atorvastatin, got %+v", rec.Stopped)
	}
	gotAdded := map[string]bool{}
	for _, m := range rec.Added {
		gotAdded[m.Name] = true
	}
	if !gotAdded["Furosemide"] || !gotAdded["Bumetanide"] {
		t.Errorf("expected Furosemide+Bumetanide added, got %+v", rec.Added)
	}
	if len(rec.Conflict) == 0 {
		t.Errorf("expected duplicate-class conflict, got none")
	}
}
