package catalog

import "testing"

func TestInterruptedImportsAreNotLeftRunning(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, e := s.StartImportJob()
	if e != nil {
		t.Fatal(e)
	}
	b, _ := s.StartImportJob()
	if e = s.FinishImportJob(b, "imported", 12, "done"); e != nil {
		t.Fatal(e)
	}
	n, e := s.RecoverImportJobs()
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	var state string
	var processed int
	s.DB.QueryRow("SELECT state FROM import_jobs WHERE id=?", a).Scan(&state)
	if state != "interrupted" {
		t.Fatal(state)
	}
	s.DB.QueryRow("SELECT processed FROM import_jobs WHERE id=?", b).Scan(&processed)
	if processed != 12 {
		t.Fatal("completed import changed")
	}
	n, e = s.RecoverImportJobs()
	if e != nil || n != 0 {
		t.Fatal("recovery not idempotent", n, e)
	}
}
