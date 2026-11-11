package catalog

import (
	"antennarr/internal/ingest"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Store struct {
	// Bulk imports yield between batches so live health writes cannot time out.
	catalogueWrites sync.Mutex
	allowUHD        atomic.Bool
	rosterPolicy    atomic.Pointer[RosterPolicy]
	DB              *sql.DB
	root            string
}
type Account struct {
	ID                int64  `json:"id"`
	Provider          string `json:"provider"`
	Kind              string `json:"kind"`
	State             string `json:"state"`
	Limit             int    `json:"connection_limit"`
	LastCheck         int64  `json:"last_check"`
	Streams           int    `json:"streams"`
	Created           int64  `json:"created"`
	Failures          int    `json:"consecutive_failures"`
	SuccessfulStreams int    `json:"successful_streams"`
	HealthyStreams    int    `json:"healthy_streams"`
	SuccessfulChecks  int    `json:"successful_account_checks"`
}
type Channel struct {
	Station string   `json:"station,omitempty"`
	ID      string   `json:"id"`
	Number  string   `json:"number"`
	Name    string   `json:"name"`
	Region  string   `json:"region"`
	Enabled bool     `json:"enabled"`
	Sources int      `json:"sources"`
	Aliases []string `json:"aliases"`
}

func Open(root string) (*Store, error) { return open(root, false) }

// OpenWithUSDefaults seeds public channel and removal defaults only for a new catalogue.
func OpenWithUSDefaults(root string) (*Store, error) { return open(root, true) }

func open(root string, defaults bool) (*Store, error) {
	for _, d := range []string{"database", "secrets", "backups"} {
		if e := os.MkdirAll(filepath.Join(root, d), 0700); e != nil {
			return nil, e
		}
	}
	dbpath := filepath.Join(root, "database/antennarr.db")
	u := url.URL{Scheme: "file", Path: dbpath}
	q := url.Values{}
	q.Set("_txlock", "immediate")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	// Catalogue sorting must not exhaust a container's small /tmp tmpfs.
	// Working sets shrink after bounded reserve cleanup; no video is stored here.
	q.Add("_pragma", "temp_store(MEMORY)")
	q.Add("_pragma", "busy_timeout(30000)")
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(4)
	fail := func(err error) (*Store, error) { db.Close(); return nil, err }
	var version int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		return fail(e)
	}
	if version > 8 {
		return fail(errors.New("database requires a newer application"))
	}
	if version > 0 && version < 8 {
		return fail(errors.New("database predates v1; upgrade it with Beta 2 before opening it here"))
	}
	if version == 0 {
		var existing int
		if e = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&existing); e != nil {
			return fail(e)
		}
		if existing != 0 {
			return fail(errors.New("unversioned database contains existing objects; refusing to replace it"))
		}
		tx, err := db.Begin()
		if err != nil {
			return fail(err)
		}
		if _, err = tx.Exec(schema); err == nil && defaults {
			_, err = tx.Exec("INSERT INTO settings(name,value) VALUES('initial_channel_preset','US')")
		}
		if err == nil && defaults {
			// Seed once in the new-database transaction. Existing policies, including
			// deliberately disabled removal, must never change on an image update.
			_, err = tx.Exec(`INSERT INTO settings(name,value) VALUES('removal_policy','{"account_failures":2,"stream_failures":3}')`)
		}
		if err == nil {
			_, err = tx.Exec("PRAGMA user_version=8")
		}
		if err != nil {
			tx.Rollback()
			return fail(err)
		}
		if err = tx.Commit(); err != nil {
			return fail(err)
		}
	}

	// Reverse mapping lookups drive stream scheduling. Without this index the
	// correlated schedule queries scan the entire catalogue for every stream.
	if _, e = db.Exec("CREATE INDEX IF NOT EXISTS mappings_stream ON mappings(stream_id,enabled,channel_id)"); e != nil {
		return fail(e)
	}
	for _, q := range []string{"CREATE INDEX IF NOT EXISTS validations_stream ON validations(stream_id)", "CREATE INDEX IF NOT EXISTS events_source ON events(source_id)"} {
		if _, e = db.Exec(q); e != nil {
			return fail(e)
		}
	}
	if e = os.Chmod(dbpath, 0600); e != nil {
		return fail(e)
	}
	store := &Store{DB: db, root: root}
	if e = store.ensureAccountDiagnostics(); e != nil {
		return fail(e)
	}
	if e = store.checkIntegrity(); e != nil {
		return fail(e)
	}
	if e = store.ensureRetention(); e != nil {
		return fail(e)
	}
	if e = store.seedInitialChannels(); e != nil {
		return fail(e)
	}
	if e = store.ensureCheckPass(); e != nil {
		return fail(e)
	}
	if e = store.ensureQueuePositions(); e != nil {
		return fail(e)
	}
	if _, e = store.PruneDeadProviders(); e != nil {
		return fail(e)
	}
	return store, nil
}
func (s *Store) Close() error { return s.DB.Close() }

// fingerprint is a catalogue deduplication key, never an authentication hash.
func fingerprint(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}
func (s *Store) Import(entries []ingest.Entry, provenance string) (int, error) {
	if len(entries) == 0 || len(entries) > 20000 {
		return 0, errors.New("invalid entry count")
	}
	if len(provenance) > 120 || strings.Contains(provenance, "://") {
		return 0, errors.New("use a short provenance label without URLs")
	}
	// Validate all endpoints before committing any part of a malformed list.
	for _, entry := range entries {
		endpoint := entry.Endpoint
		if endpoint == "" {
			endpoint = entry.URL
		}
		u, e := url.Parse(endpoint)
		if e != nil || u.Host == "" {
			return 0, errors.New("invalid endpoint")
		}
	}
	total := 0
	for start := 0; start < len(entries); start += 100 {
		n, e := s.importBatch(entries[start:min(start+100, len(entries))], provenance)
		total += n
		if e != nil {
			return total, e
		}
	}
	return total, nil
}

// Earlier committed batches remain on a later database error; retrying is
// idempotent. Each batch keeps stream metadata and its mappings atomic.
func (s *Store) importBatch(entries []ingest.Entry, provenance string) (int, error) {
	s.catalogueWrites.Lock()
	defer s.catalogueWrites.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	count := 0
	changed := []int64{}
	for _, entry := range entries {
		endpoint := entry.Endpoint
		if endpoint == "" {
			endpoint = entry.URL
		}
		u, e := url.Parse(endpoint)
		if e != nil || u.Host == "" {
			return 0, errors.New("invalid endpoint")
		}
		identity := fingerprint(entry.Kind + "\x00" + entry.Endpoint + "\x00" + entry.Username)
		protected, e := json.Marshal(entry)
		if e != nil {
			return 0, e
		}
		_, e = tx.Exec(`INSERT INTO accounts(id,identity,provider,kind,secret,created,rate_key) VALUES((SELECT max(coalesce((SELECT max(id) FROM accounts),0),coalesce((SELECT CAST(value AS INTEGER) FROM settings WHERE name='deleted_accounts_highwater'),0))+1),?,?,?,?,?,?) ON CONFLICT(identity) DO UPDATE SET secret=excluded.secret,rate_key=excluded.rate_key`, identity, u.Host, entry.Kind, protected, now, ProviderKey(u.Host))
		if e != nil {
			return 0, e
		}
		var aid int64
		if e = tx.QueryRow("SELECT id FROM accounts WHERE identity=?", identity).Scan(&aid); e != nil {
			return 0, e
		}
		_, e = tx.Exec(`INSERT INTO provenance VALUES(?,?,?,?,?) ON CONFLICT(account_id,fingerprint) DO UPDATE SET last_seen=excluded.last_seen`, aid, fingerprint(provenance), provenance, now, now)
		if e != nil {
			return 0, e
		}
		if entry.URL != "" {
			identity := streamIdentity(entry)
			var oldID int64
			var oldName, oldTVG, oldRegion string
			e = tx.QueryRow("SELECT id,name,tvg_id,region FROM streams WHERE identity=?", identity).Scan(&oldID, &oldName, &oldTVG, &oldRegion)
			if e != nil && e != sql.ErrNoRows {
				return 0, e
			}
			var id int64
			e = tx.QueryRow(`INSERT INTO streams(id,account_id,identity,name,tvg_id,region,secret) VALUES((SELECT max(coalesce((SELECT max(id) FROM streams),0),coalesce((SELECT CAST(value AS INTEGER) FROM settings WHERE name='deleted_streams_highwater'),0))+1),?,?,?,?,?,?) ON CONFLICT(identity) DO UPDATE SET name=excluded.name,tvg_id=excluded.tvg_id,region=excluded.region,secret=excluded.secret RETURNING id`, aid, identity, entry.Name, entry.TVGID, entry.Region, protected).Scan(&id)
			if e != nil {
				return 0, e
			}
			count++
			if oldID == 0 || oldName != entry.Name || oldTVG != entry.TVGID || oldRegion != entry.Region {
				changed = append(changed, id)
			}
		} else {
			count++
		}
	}
	if e = autoMapStreams(tx, changed); e != nil {
		return 0, e
	}
	return count, tx.Commit()
}

func (s *Store) Channels() ([]Channel, error) {
	rows, e := s.DB.Query(`SELECT c.id,c.number,c.name,c.region,c.enabled,count(m.stream_id),c.aliases,coalesce((SELECT value FROM settings WHERE name='channel_station:'||c.id),'') FROM channels c LEFT JOIN mappings m ON m.channel_id=c.id AND m.enabled=1 GROUP BY c.id ORDER BY CAST(c.number AS REAL)`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		var c Channel
		var aliases string
		if e = rows.Scan(&c.ID, &c.Number, &c.Name, &c.Region, &c.Enabled, &c.Sources, &aliases, &c.Station); e != nil {
			return nil, e
		}
		json.Unmarshal([]byte(aliases), &c.Aliases)
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveChannel retains an existing permanent ID when its display name or number
// changes. Matching and primary output assignments follow the committed edit;
// callers must not replace identity merely to change its Plex presentation.
func (s *Store) SaveChannel(c Channel) (string, error) {
	c.Station = normaliseStation(c.Station)
	if !validStation(c.Station) {
		return "", errors.New("invalid station ID; use up to 32 letters, digits, dots, or hyphens")
	}
	c.Name = strings.TrimSpace(c.Name)
	c.Region = strings.TrimSpace(c.Region)
	if c.Name == "" || utf8.RuneCountInString(c.Name) > 100 || utf8.RuneCountInString(c.Region) > 80 {
		return "", errors.New("invalid channel name or region")
	}
	if _, e := strconv.ParseFloat(c.Number, 64); e != nil || len(c.Number) > 10 || !regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`).MatchString(c.Number) {
		return "", errors.New("invalid channel number")
	}
	if c.ID == "" {
		b := make([]byte, 12)
		if _, e := rand.Read(b); e != nil {
			return "", e
		}
		c.ID = hex.EncodeToString(b)
	}
	if len(c.Aliases) > 200 {
		return "", errors.New("maximum 200 provider-name rules per channel")
	}
	for _, a := range c.Aliases {
		if utf8.RuneCountInString(a) > 100 {
			return "", errors.New("alias too long")
		}
	}
	aliases, _ := json.Marshal(c.Aliases)
	tx, e := s.DB.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var count, exists int
	if e = tx.QueryRow("SELECT count(*) FROM channels").Scan(&count); e != nil {
		return "", e
	}
	if e = tx.QueryRow("SELECT count(*) FROM channels WHERE id=?", c.ID).Scan(&exists); e != nil {
		return "", e
	}
	if count >= MaxSelectedChannels && exists == 0 {
		return "", fmt.Errorf("maximum %d selected channels", MaxSelectedChannels)
	}
	var previousName, previousRegion, previousAliases, previousStation string
	previousStation, e = readStation(tx, c.ID)
	if e != nil {
		return "", e
	}
	_ = tx.QueryRow("SELECT name,region,aliases FROM channels WHERE id=?", c.ID).Scan(&previousName, &previousRegion, &previousAliases)
	refresh := previousStation != c.Station || exists == 0 || previousName != c.Name || previousRegion != c.Region || previousAliases != string(aliases)
	_, e = tx.Exec(`INSERT INTO channels(id,number,name,region,enabled,aliases) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET number=excluded.number,name=excluded.name,region=excluded.region,enabled=excluded.enabled,aliases=excluded.aliases`, c.ID, c.Number, c.Name, c.Region, c.Enabled, string(aliases))
	if e != nil {
		return "", errors.New("channel could not be saved; check number uniqueness")
	}
	if e = saveStation(tx, c.ID, c.Station); e != nil {
		return "", e
	}
	if refresh {
		previous := Channel{ID: c.ID, Name: previousName, Region: previousRegion, Station: previousStation}
		json.Unmarshal([]byte(previousAliases), &previous.Aliases)
		if e = autoMapChannel(tx, previous, c); e != nil {
			return "", e
		}
	}
	if refresh {
		if _, e = tx.Exec("UPDATE accounts SET next_check=0,catalogue_checked=0 WHERE state NOT IN ('manually_blocked','retired')"); e != nil {
			return "", e
		}
	}
	return c.ID, tx.Commit()
}

func (s *Store) Map(channel string, stream int64, enabled bool) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if enabled {
		var c Channel
		var raw, name, tvgID, region string
		c.ID = channel
		c.Station, e = readStation(tx, channel)
		if e != nil {
			return e
		}
		if e := tx.QueryRow("SELECT name,region,aliases FROM channels WHERE id=?", channel).Scan(&c.Name, &c.Region, &raw); e != nil {
			return errors.New("channel not found")
		}
		if e := tx.QueryRow("SELECT name,tvg_id,region FROM streams WHERE id=?", stream).Scan(&name, &tvgID, &region); e != nil {
			return errors.New("stream not found")
		}
		json.Unmarshal([]byte(raw), &c.Aliases)
		label := sourceLabelWithID(name, region, tvgID)
		m := compileChannel(c)
		channels, err := readChannelMatchers(tx)
		if err != nil {
			return err
		}
		if m.excludes(label) || !m.stationMatches(label) || guideConflict(channels, m, label) {
			return errors.New("stream excluded by channel rules; edit the channel rules first")
		}
	}
	if _, e = tx.Exec(`INSERT INTO mappings(channel_id,stream_id,enabled,origin) VALUES(?,?,?,'manual') ON CONFLICT(channel_id,stream_id) DO UPDATE SET enabled=excluded.enabled,origin='manual'`, channel, stream, enabled); e != nil {
		return e
	}
	if enabled {
		if _, e = tx.Exec(`INSERT OR IGNORE INTO channel_aliases(channel_id,alias) SELECT ?,'tvg:'||lower(tvg_id) FROM streams WHERE id=? AND tvg_id!=''`, channel, stream); e != nil {
			return e
		}
		// Learning a TVG identity affects only streams advertising that identity.
		rows, e := tx.Query("SELECT id FROM streams WHERE tvg_id!='' AND lower(tvg_id)=(SELECT lower(tvg_id) FROM streams WHERE id=?)", stream)
		if e != nil {
			return e
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if e = autoMapStreams(tx, ids); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteChannel(id string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old Channel
	var raw string
	old.ID = id
	old.Station, e = readStation(tx, id)
	if e != nil {
		return e
	}
	if e = tx.QueryRow("SELECT name,region,aliases FROM channels WHERE id=?", id).Scan(&old.Name, &old.Region, &raw); e != nil && e != sql.ErrNoRows {
		return e
	}
	json.Unmarshal([]byte(raw), &old.Aliases)
	affected, e := changedChannelStreams(tx, old, Channel{ID: id})
	if e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM channel_aliases WHERE channel_id=?", id); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM channels WHERE id=?", id); e != nil {
		return e
	}
	if e = saveStation(tx, id, ""); e != nil {
		return e
	}
	if e = autoMapStreams(tx, affected); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) AccountSecret(id int64) (ingest.Entry, error) {
	var entry ingest.Entry
	var b []byte
	e := s.DB.QueryRow("SELECT secret FROM accounts WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &entry)
	}
	return entry, e
}
func (s *Store) Backup() (string, error) {
	name := fmt.Sprintf("catalogue-%d.db", time.Now().UnixNano())
	path := filepath.Join(s.root, "backups", name)
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
		}
	}()
	_, e := s.DB.Exec("VACUUM INTO '" + strings.ReplaceAll(path, "'", "''") + "'")
	if e != nil {
		return "", e
	}
	if e = os.Chmod(path, 0600); e != nil {
		return "", e
	}
	complete = true
	return name, nil
}

func streamIdentity(entry ingest.Entry) string {
	if entry.Kind == "xtream" && entry.Username != "" && entry.URL != "" {
		u, e := url.Parse(entry.URL)
		if e == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) > 0 {
				id := parts[len(parts)-1]
				id = strings.TrimSuffix(strings.TrimSuffix(id, ".ts"), ".m3u8")
				return fingerprint("xtream-stream\x00" + entry.Endpoint + "\x00" + entry.Username + "\x00" + id)
			}
		}
	}
	return fingerprint(entry.URL)
}
