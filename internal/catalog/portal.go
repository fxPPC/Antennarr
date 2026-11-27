package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"antennarr/internal/ingest"
)

func portalIdentity(a ingest.PortalAccount) string {
	return fingerprint("portal\x00" + a.PortalURL + "\x00" + a.MAC)
}

func (s *Store) PortalAccountID(a ingest.PortalAccount) (int64, error) {
	var id int64
	err := s.DB.QueryRow("SELECT id FROM accounts WHERE identity=? AND kind='portal'", portalIdentity(a)).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func (s *Store) PortalAccount(id int64) (ingest.PortalAccount, error) {
	var account ingest.PortalAccount
	var secret []byte
	err := s.DB.QueryRow("SELECT secret FROM accounts WHERE id=? AND kind='portal' AND state NOT IN ('manually_blocked','retired')", id).Scan(&secret)
	if err != nil {
		return account, errors.New("portal account unavailable")
	}
	if err = json.Unmarshal(secret, &account); err != nil {
		return account, errors.New("invalid portal account configuration")
	}
	return account, nil
}

// An entire authorised catalogue is replaced in one transaction. Published
// candidate identifiers survive refreshes and rotating signed media links.
func (s *Store) SavePortalCatalogue(a ingest.PortalAccount, channels []ingest.PortalChannel) (int64, error) {
	if err := a.Normalise(); err != nil {
		return 0, err
	}
	u, _ := url.Parse(a.PortalURL)
	secret, err := json.Marshal(a)
	if err != nil {
		return 0, err
	}
	s.catalogueWrites.Lock()
	defer s.catalogueWrites.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow(`INSERT INTO accounts(id,identity,provider,kind,secret,created,rate_key,catalogue_checked,connection_limit)
	 VALUES((SELECT max(coalesce((SELECT max(id) FROM accounts),0),coalesce((SELECT CAST(value AS INTEGER) FROM settings WHERE name='deleted_accounts_highwater'),0))+1),?,?,'portal',?,unixepoch(),?,unixepoch(),1)
	 ON CONFLICT(identity) DO UPDATE SET secret=excluded.secret,catalogue_checked=excluded.catalogue_checked RETURNING id`,
		portalIdentity(a), u.Host, secret, ProviderKey(u.Host)).Scan(&id)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec("CREATE TEMP TABLE portal_imported_streams(id INTEGER PRIMARY KEY)")
	if err != nil {
		return 0, err
	}
	changed := make([]int64, 0, len(channels))
	for _, c := range channels {
		ref := "portal:" + strconv.FormatInt(id, 10) + "/" + c.Key
		record, _ := json.Marshal(ingest.Entry{Kind: "portal", Name: c.Name, TVGID: c.TVGID, URL: ref, Endpoint: a.PortalURL})
		var sid int64
		err = tx.QueryRow(`INSERT INTO streams(id,account_id,identity,name,tvg_id,secret)
		 VALUES((SELECT max(coalesce((SELECT max(id) FROM streams),0),coalesce((SELECT CAST(value AS INTEGER) FROM settings WHERE name='deleted_streams_highwater'),0))+1),?,?,?,?,?)
		 ON CONFLICT(identity) DO UPDATE SET name=excluded.name,tvg_id=excluded.tvg_id,secret=excluded.secret,
		 enabled=CASE WHEN streams.state='portal_unsubscribed' THEN 1 ELSE streams.enabled END,
		 state=CASE WHEN streams.state='portal_unsubscribed' THEN 'unverified' ELSE streams.state END RETURNING id`,
			id, fingerprint(ref), c.Name, c.TVGID, record).Scan(&sid)
		if err != nil {
			return 0, err
		}
		if _, err = tx.Exec("INSERT INTO portal_imported_streams VALUES(?)", sid); err != nil {
			return 0, err
		}
		changed = append(changed, sid)
	}
	if _, err = tx.Exec("UPDATE streams SET enabled=0,state='portal_unsubscribed' WHERE account_id=? AND enabled=1 AND id NOT IN (SELECT id FROM portal_imported_streams)", id); err != nil {
		return 0, err
	}
	if _, err = tx.Exec("DELETE FROM roster WHERE account_id=? AND stream_id NOT IN (SELECT id FROM portal_imported_streams)", id); err != nil {
		return 0, err
	}
	if err = autoMapStreams(tx, changed); err != nil {
		return 0, err
	}
	// TEMP tables outlive transactions on pooled SQLite connections.
	if _, err = tx.Exec("DROP TABLE portal_imported_streams"); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// Resolution runs after the caller has reserved the account's connection.
// This ensures a stale, expired, or removed link cannot be used from the DB.
func (s *Store) ResolvePortalStream(ctx context.Context, client *http.Client, ref string) (string, error) {
	if !strings.HasPrefix(ref, "portal:") {
		return ref, nil
	}
	accountID, key, ok := strings.Cut(strings.TrimPrefix(ref, "portal:"), "/")
	id, err := strconv.ParseInt(accountID, 10, 64)
	if err != nil || id < 1 || !ok || len(key) != 64 {
		return "", errors.New("invalid portal stream identity")
	}
	a, err := s.PortalAccount(id)
	if err != nil {
		return "", err
	}
	channels, err := a.Channels(ctx, client)
	if err != nil {
		return "", err
	}
	for _, c := range channels {
		if c.Key == key {
			return c.URL, nil
		}
	}
	return "", errors.New("channel is no longer available on this subscription")
}
