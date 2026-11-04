package catalog

type QueueAccountRow struct {
	ID       int64  `json:"id"`
	Provider string `json:"provider"`
	State    string `json:"state"`
	Created  int64  `json:"created"`
	Streams  int    `json:"streams"`
	Roster   int    `json:"roster"`
}

func (s *Store) RecentRosterEvents() ([]map[string]any, error) {
	rows, e := s.DB.Query("SELECT happened,kind,channel_id,source_id,reason FROM events ORDER BY id DESC LIMIT 30")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var t, id int64
		var kind, ch, reason string
		if e = rows.Scan(&t, &kind, &ch, &id, &reason); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"happened": t, "kind": kind, "channel_id": ch, "stream_id": id, "reason": reason})
	}
	return out, rows.Err()
}

func (s *Store) ExcludedStreams(channel string) ([]map[string]any, error) {
	rows, e := s.DB.Query(`SELECT s.id,s.account_id,s.name,a.provider,s.state FROM mappings m JOIN streams s ON s.id=m.stream_id JOIN accounts a ON a.id=s.account_id WHERE m.channel_id=? AND m.enabled=0 ORDER BY s.name,s.id LIMIT 50`, channel)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, account int64
		var name, provider, state string
		if e = rows.Scan(&id, &account, &name, &provider, &state); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"stream_id": id, "account_id": account, "name": name, "provider": provider, "state": state})
	}
	return out, rows.Err()
}

// AccountPage is read in one SQLite snapshot. Offsets are clamped after pruning
// so deleting the final rows cannot strand the UI on a permanently empty page.
type AccountPage struct {
	Items   []QueueAccountRow `json:"items"`
	Total   int               `json:"total"`
	Offset  int               `json:"offset"`
	Limit   int               `json:"limit"`
	HasNext bool              `json:"has_next"`
}

func (s *Store) InputPage(search string, offset int) (AccountPage, error) {
	out := AccountPage{Items: []QueueAccountRow{}, Limit: 50}
	where, args, e := inputSearch(search)
	if e != nil {
		return out, e
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = tx.QueryRow("SELECT count(*) FROM accounts a"+where, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	out.Offset = max(0, min(offset, max(0, (out.Total-1)/50)*50))
	rows, e := tx.Query(`SELECT a.id,a.provider,a.state,a.created,(SELECT count(*) FROM streams s WHERE s.account_id=a.id),(SELECT count(*) FROM roster r WHERE r.account_id=a.id) FROM accounts a`+where+` ORDER BY a.provider COLLATE NOCASE,a.id LIMIT 50 OFFSET ?`, append(args, out.Offset)...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var a QueueAccountRow
		if e = rows.Scan(&a.ID, &a.Provider, &a.State, &a.Created, &a.Streams, &a.Roster); e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	out.HasNext = out.Offset+len(out.Items) < out.Total
	return out, tx.Commit()
}
