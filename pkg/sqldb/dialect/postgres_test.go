package dialect

import (
	"bytes"
	"testing"
)

func TestPostgres_Name(t *testing.T) {
	p := Postgres{}
	if got := p.Name(); got != "postgres" {
		t.Fatalf("Name() = %q, want %q", got, "postgres")
	}
}

func TestPostgres_Paginate(t *testing.T) {
	p := Postgres{}
	var buf bytes.Buffer
	buf.WriteString("SELECT * FROM foo")
	p.Paginate(&buf)
	got := buf.String()
	want := "SELECT * FROM foo OFFSET :offset ROWS FETCH NEXT :rows_per_page ROWS ONLY"
	if got != want {
		t.Fatalf("Paginate:\ngot  %q\nwant %q", got, want)
	}
}

// TestPostgres_ImplementsDialect ensures Postgres satisfies the Dialect interface.
func TestPostgres_ImplementsDialect(t *testing.T) {
	var _ Dialect = Postgres{}
}
