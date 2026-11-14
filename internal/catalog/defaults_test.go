package catalog

import (
	"fmt"
	"testing"
)

func TestFreshUSDefaultsPreserveEdits(t *testing.T) {
	root := t.TempDir()
	s, err := OpenWithUSDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.ChannelsForProfile("primary")
	if err != nil || len(ch) != 150 {
		t.Fatalf("channels=%d err=%v", len(ch), err)
	}
	var accounts int
	s.DB.QueryRow("SELECT count(*) FROM accounts").Scan(&accounts)
	if accounts != 0 {
		t.Fatal("preset contains accounts")
	}
	if _, err = s.DB.Exec("DELETE FROM channels"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenWithUSDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ch, _ = s.Channels()
	if len(ch) != 0 {
		t.Fatal("deleted channels restored")
	}
}
func TestExistingEmptyCatalogueNotSeeded(t *testing.T) {
	root := t.TempDir()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = OpenWithUSDefaults(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ch, _ := s.Channels()
	if len(ch) != 0 {
		t.Fatal("existing setup changed")
	}
}

func TestApplyUSPresetAlignsGuideAndPreservesIDs(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, c := range []Channel{{ID: "personal-cnn", Name: "CNN", Number: "101", Region: "US", Enabled: true}, {ID: "personal-mtv", Name: "MTV", Number: "102", Region: "US", Enabled: true}, {ID: "personal-fox", Name: "FOX", Number: "005", Region: "US", Enabled: true}} {
		if _, e = s.SaveChannel(c); e != nil {
			t.Fatal(e)
		}
	}
	result, e := s.ApplyUSPreset()
	if e != nil || result.Added != 147 || result.Updated != 3 {
		t.Fatal(result, e)
	}
	channels, e := s.Channels()
	if e != nil || len(channels) != 150 {
		t.Fatal(len(channels), e)
	}
	found := map[string]Channel{}
	for _, c := range channels {
		found[c.Name] = c
	}
	if found["CNN"].ID != "personal-cnn" || found["CNN"].Number != "078" || found["FOX"].ID != "personal-fox" || found["FOX"].Number != "005" {
		t.Fatal("personal identity changed")
	}
	if found["USA Network"].Number != "016" || found["FXX"].Number != "109" {
		t.Fatal("guide numbers not used")
	}
	result, e = s.ApplyUSPreset()
	if e != nil || result.Added != 0 || result.Updated != 150 {
		t.Fatal("preset not idempotent", result, e)
	}
}

func TestApplyUSPresetRollbackOnCapacity(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	// Populate unrelated channels without repeatedly rebuilding mappings.
	tx, e := s.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 114; i++ {
		if _, e = tx.Exec("INSERT INTO channels(id,number,name,region,enabled,aliases) VALUES(?,?,?,'US',1,'[]')", fmt.Sprint(i), fmt.Sprint(2000+i), fmt.Sprintf("Custom %d", i)); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyUSPreset(); e == nil {
		t.Fatal("oversized merge accepted")
	}
	cs, e := s.Channels()
	if e != nil || len(cs) != 114 {
		t.Fatal("partial preset committed", len(cs), e)
	}
}

func TestGuideNumberConflictRollsBackRenumbering(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, c := range []Channel{{ID: "cnn", Name: "CNN", Number: "101", Region: "US", Enabled: true}, {ID: "custom", Name: "Custom Local", Number: "109", Region: "US", Enabled: true}} {
		if _, e = s.SaveChannel(c); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.ApplyUSPreset(); e == nil {
		t.Fatal("unrelated channel number collision ignored")
	}
	cs, e := s.Channels()
	if e != nil || len(cs) != 2 {
		t.Fatal("partial merge", cs, e)
	}
	for _, c := range cs {
		if c.ID == "cnn" && c.Number != "101" {
			t.Fatal("temporary renumbering escaped transaction", c)
		}
	}
}

func TestFreshRemovalPolicyDoesNotOverwriteSavedPolicy(t *testing.T) {
	root := t.TempDir()
	s, err := OpenWithUSDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := s.RemovalPolicy()
	if err != nil || policy != (RemovalPolicy{2, 3}) {
		t.Fatal(policy, err)
	}
	if err = s.SaveRemovalPolicy(RemovalPolicy{}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenWithUSDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	policy, err = s.RemovalPolicy()
	if err != nil || policy != (RemovalPolicy{}) {
		t.Fatal("existing policy changed", policy, err)
	}
}
