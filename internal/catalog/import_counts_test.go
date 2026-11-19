package catalog

import (
	"fmt"
	"testing"

	"antennarr/internal/ingest"
)

func TestImportReceiptCountsDistinctAccountsAcrossBatches(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var list []ingest.Entry
	for i := 0; i < 105; i++ {
		entry, err := ingest.FromURL(fmt.Sprintf("https://fixture-%d.example/a.ts", i), "Fixture", "")
		if err != nil {
			t.Fatal(err)
		}
		list = append(list, entry)
	}
	if _, err = s.Import(list[:2], "fixture"); err != nil {
		t.Fatal(err)
	}
	list = append(list, list[0], list[2])
	n, receipt, err := s.ImportWithCounts(list, "fixture")
	if err != nil || n != 107 || receipt != (ImportCounts{Added: 103, Existing: 2}) {
		t.Fatalf("processed %d, receipt %+v, error %v", n, receipt, err)
	}
}
