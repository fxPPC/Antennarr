package catalog

import (
	"antennarr/internal/ingest"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDedupHistoryMappingAndBackup(t *testing.T) {
	root := t.TempDir()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rows, e := ingest.Parse([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"cnn.us\",CNN US\nhttps://tv.example/live/alice/secret-password/42.ts\n"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Import(rows, "My subscription"); e != nil {
		t.Fatal(e)
	}
	accounts, _ := s.Accounts()
	streams, _ := s.Streams("CNN")
	if len(accounts) != 1 || len(streams) != 1 {
		t.Fatal(accounts, streams)
	}
	sid := streams[0].ID
	_, e = s.DB.Exec("UPDATE streams SET successes=8,score=0.9 WHERE id=?", sid)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Import(rows, "Second authorised import"); e != nil {
		t.Fatal(e)
	}
	streams, _ = s.Streams("")
	if len(streams) != 1 || streams[0].ID != sid || streams[0].Score != 0.9 {
		t.Fatal("history lost", streams)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM provenance").Scan(&n)
	if n != 2 {
		t.Fatal("provenance lost")
	}
	c := Channel{Number: "101", Name: "CNN US", Region: "US domestic", Enabled: true}
	id, e := s.SaveChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Map(id, sid, true); e != nil {
		t.Fatal(e)
	}
	c.ID = id
	c.Number = "102"
	updated, e := s.SaveChannel(c)
	if e != nil || updated != id {
		t.Fatal("unstable identity")
	}
	if e = s.Quarantine(accounts[0].ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Import(rows, "My subscription"); e != nil {
		t.Fatal(e)
	}
	accounts, _ = s.Accounts()
	if accounts[0].State != "manually_blocked" {
		t.Fatal("reimport removed block")
	}
	name, e := s.Backup()
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(root, "backups", name))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(raw, []byte("secret-password")) {
		t.Fatal("plain stored record missing from backup")
	}
	st, err := os.Stat(filepath.Join(root, "database/antennarr.db"))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("database permissions")
	}
	if _, err = os.Stat(filepath.Join(root, "secrets/catalogue-key")); !os.IsNotExist(err) {
		t.Fatal("unexpected encryption key")
	}
	view, _ := json.Marshal(accounts)
	if bytes.Contains(view, []byte("secret-password")) {
		t.Fatal("API secret leak")
	}
	a, e := s.AccountSecret(accounts[0].ID)
	if e != nil || a.Password != "secret-password" {
		t.Fatal("secret recovery failed")
	}
	if e = s.DeleteChannel(id); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT count(*) FROM mappings").Scan(&n)
	if n != 0 {
		t.Fatal("orphan mapping")
	}
}
func TestMigrationRestartAndChannelLimit(t *testing.T) {
	root := t.TempDir()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < MaxSelectedChannels; i++ {
		_, e = s.SaveChannel(Channel{Number: strconv.Itoa(i + 1), Name: "Test", Enabled: true})
		if e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.SaveChannel(Channel{Number: "999", Name: "Extra"}); e == nil {
		t.Fatal("channel cap not enforced")
	}
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var version int
	s.DB.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 8 {
		t.Fatal(version)
	}
	if _, e = s.SaveChannel(Channel{Number: "NaN", Name: "Invalid"}); e == nil {
		t.Fatal("invalid channel number accepted")
	}
}

func TestConfirmedTVGIDMapsFutureAccountsAndPreservesDisabledMatch(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	first, _ := ingest.Parse([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"CNN.us\",CNN US\nhttps://one.example/live/u/p/1.ts\n"))
	s.Import(first, "one")
	rows, _ := s.Streams("")
	channel, _ := s.SaveChannel(Channel{Number: "1", Name: "CNN US", Enabled: true})
	if e = s.Map(channel, rows[0].ID, true); e != nil {
		t.Fatal(e)
	}
	second, _ := ingest.Parse([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"CNN.us\",CNN US\nhttps://two.example/live/u/p/4.ts\n#EXTINF:-1 tvg-id=\"CNN.international\",CNN International\nhttps://two.example/live/u/p/5.ts\n"))
	s.Import(second, "two")
	channels, _ := s.Channels()
	if channels[0].Sources != 2 {
		t.Fatal("new account did not inherit confirmed TVG mapping", channels)
	}
	all, _ := s.Streams("")
	for _, v := range all {
		if v.Provider == "two.example" && v.TVGID == "CNN.us" {
			s.Map(channel, v.ID, false)
		}
	}
	s.Import(second, "repeat")
	channels, _ = s.Channels()
	if channels[0].Sources != 1 {
		t.Fatal("disabled match re-enabled")
	}
}

func TestXtreamPasswordRotationRetainsStreamIdentityAndHistory(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rows, _ := ingest.Parse([]byte("https://provider.example/live/user/old-test-password/42.ts"))
	s.Import(rows, "original")
	streams, _ := s.Streams("")
	id := streams[0].ID
	s.DB.Exec("UPDATE streams SET score=0.99,successes=8 WHERE id=?", id)
	rows, _ = ingest.Parse([]byte("https://provider.example/live/user/new-test-password/42.ts"))
	s.Import(rows, "updated credential")
	streams, _ = s.Streams("")
	if len(streams) != 1 || streams[0].ID != id || streams[0].Score != 0.99 {
		t.Fatal("credential rotation lost history", streams)
	}
	var raw []byte
	s.DB.QueryRow("SELECT secret FROM streams WHERE id=?", id).Scan(&raw)
	if !bytes.Contains(raw, []byte("new-test-password")) || bytes.Contains(raw, []byte("old-test-password")) {
		t.Fatal("stream credentials not updated")
	}
}
