package catalog

import "encoding/json"

// ReplaceChannels is one transaction. Matching default IDs survive a default
// reset; accounts and streams are never part of this operation.
func (s *Store) ReplaceChannels(defaultUS bool) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	old := map[string]string{}
	rows, e := tx.Query("SELECT id,name,region FROM channels")
	if e != nil {
		return e
	}
	for rows.Next() {
		var id, name, region string
		if e = rows.Scan(&id, &name, &region); e != nil {
			rows.Close()
			return e
		}
		key, _ := channelKey(name, region)
		old[key] = id
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, q := range []string{"DELETE FROM channel_aliases", "DELETE FROM channels", "DELETE FROM settings WHERE name IN('roster_cache_uhd_true','roster_cache_uhd_false')"} {
		if _, e = tx.Exec(q); e != nil {
			return e
		}
	}
	if defaultUS {
		preset, err := DefaultUSChannels()
		if err != nil {
			return err
		}
		for _, c := range preset {
			key, _ := channelKey(c.Name, c.Region)
			id := old[key]
			if id == "" {
				id = "us-default-" + c.Number
			}
			raw, _ := json.Marshal(c.Aliases)
			if _, e = tx.Exec("INSERT INTO channels(id,number,name,region,enabled,aliases) VALUES(?,?,?,'US',1,?)", id, c.Number, c.Name, string(raw)); e != nil {
				return e
			}
		}
		if _, e = tx.Exec("UPDATE accounts SET catalogue_checked=0 WHERE kind='xtream' AND state NOT IN ('retired','manually_blocked')"); e != nil {
			return e
		}
		if e = autoMap(tx); e != nil {
			return e
		}
	}
	return tx.Commit()
}
