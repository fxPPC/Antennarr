package catalog

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// Bump this marker whenever matching semantics change. Existing v1 catalogues
// receive one full refresh; subsequent starts reuse committed assignments.
const matchingRevision = "v1-incremental-2"

func autoMap(tx *sql.Tx) error {
	if e := autoMapStreams(tx, nil); e != nil {
		return e
	}
	_, e := tx.Exec("INSERT INTO settings(name,value) VALUES('matching_revision',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", matchingRevision)
	return e
}

// nil means a full refresh; an empty slice means no changed stream metadata.
// Scope lives inside the caller's transaction, so mappings and imports commit
// together. Explicit exclusions survive; negative rules override enabled maps.
func autoMapStreams(tx *sql.Tx, ids []int64) error {
	if ids != nil && len(ids) == 0 {
		return nil
	}
	if _, e := tx.Exec("CREATE TEMP TABLE rematch_scope(id INTEGER PRIMARY KEY)"); e != nil {
		return e
	}
	defer tx.Exec("DROP TABLE rematch_scope")
	if ids == nil {
		if _, e := tx.Exec("INSERT INTO rematch_scope SELECT id FROM streams"); e != nil {
			return e
		}
	} else {
		for _, id := range ids {
			if _, e := tx.Exec("INSERT OR IGNORE INTO rematch_scope VALUES(?)", id); e != nil {
				return e
			}
		}
	}
	if _, e := tx.Exec("DELETE FROM mappings WHERE origin='automatic' AND stream_id IN(SELECT id FROM rematch_scope)"); e != nil {
		return e
	}
	known, e := readLearnedChannelIDs(tx)
	if e != nil {
		return e
	}
	rows, e := tx.Query("SELECT id,number,name,region,enabled,aliases FROM channels")
	if e != nil {
		return e
	}
	var channels []channelMatcher
	byID := map[string]channelMatcher{}
	for rows.Next() {
		var c Channel
		var raw string
		if e = rows.Scan(&c.ID, &c.Number, &c.Name, &c.Region, &c.Enabled, &raw); e != nil {
			rows.Close()
			return e
		}
		json.Unmarshal([]byte(raw), &c.Aliases)
		m := compileChannel(c)
		channels = append(channels, m)
		byID[c.ID] = m
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	existing := map[int64][]string{}
	rows, e = tx.Query("SELECT m.stream_id,m.channel_id FROM mappings m JOIN rematch_scope x ON x.id=m.stream_id UNION SELECT r.stream_id,r.channel_id FROM roster r JOIN rematch_scope x ON x.id=r.stream_id")
	if e != nil {
		return e
	}
	for rows.Next() {
		var id int64
		var channel string
		if e = rows.Scan(&id, &channel); e != nil {
			rows.Close()
			return e
		}
		existing[id] = append(existing[id], channel)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	type pair struct {
		id      int64
		channel string
	}
	var add, exclude []pair
	rows, e = tx.Query("SELECT s.id,s.name,s.region,s.tvg_id FROM streams s JOIN rematch_scope x ON x.id=s.id")
	if e != nil {
		return e
	}
	for rows.Next() {
		var id int64
		var name, region, tvg string
		if e = rows.Scan(&id, &name, &region, &tvg); e != nil {
			rows.Close()
			return e
		}
		label := sourceLabelWithID(name, region, tvg)
		if c := resolveChannel(channels, label, tvg, known); c != nil {
			add = append(add, pair{id, c.channel.ID})
		}
		// Only existing assignments need deletion, not every incompatible pair in
		// the channels-by-streams cross product (millions of pointless SQL calls).
		for _, channel := range existing[id] {
			c, ok := byID[channel]
			if !ok {
				continue
			}
			if c.excludes(label) {
				exclude = append(exclude, pair{id, channel})
			}
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, m := range add {
		if _, e = tx.Exec("INSERT OR IGNORE INTO mappings(channel_id,stream_id,enabled,origin) VALUES(?,?,1,'automatic')", m.channel, m.id); e != nil {
			return e
		}
	}
	for _, m := range exclude {
		if _, e = tx.Exec("DELETE FROM mappings WHERE channel_id=? AND stream_id=? AND enabled=1", m.channel, m.id); e != nil {
			return e
		}
		if _, e = tx.Exec("DELETE FROM roster WHERE channel_id=? AND stream_id=?", m.channel, m.id); e != nil {
			return e
		}
	}
	return nil
}

// A changed channel can resolve or create ambiguity with another channel. Find
// streams touched by its old/new rules, then evaluate those against all rules.
func autoMapChannel(tx *sql.Tx, old, next Channel) error {
	ids, e := changedChannelStreams(tx, old, next)
	if e != nil {
		return e
	}
	return autoMapStreams(tx, ids)
}

func changedChannelStreams(tx *sql.Tx, old, next Channel) ([]int64, error) {
	before, after := compileChannel(old), compileChannel(next)
	assigned := map[int64]bool{}
	ids := []int64{}
	rows, e := tx.Query("SELECT stream_id FROM mappings WHERE channel_id=?", next.ID)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		assigned[id] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	learned := map[string]bool{}
	rows, e = tx.Query("SELECT alias FROM channel_aliases WHERE channel_id=?", next.ID)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var alias string
		if e = rows.Scan(&alias); e != nil {
			rows.Close()
			return nil, e
		}
		learned[alias] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = tx.Query("SELECT id,name,region,tvg_id FROM streams")
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id int64
		var n, r, t string
		if e = rows.Scan(&id, &n, &r, &t); e != nil {
			rows.Close()
			return nil, e
		}
		label := sourceLabelWithID(n, r, t)
		if assigned[id] || before.matches(label) || after.matches(label) || learned["tvg:"+strings.ToLower(t)] {
			ids = append(ids, id)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return ids, nil
}
