package catalog

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidSearch identifies a rejected user query without exposing SQL errors.
var ErrInvalidSearch = errors.New("invalid search; use up to 120 characters and positive account/stream IDs")

var idSearch = regexp.MustCompile(`(?i)^(account|stream)\s*[:#]\s*([0-9]+)$`)

// Explicit ID searches keep account 42 distinct from stream 42. Text is always
// bound as SQL data; wildcard characters are treated literally.
func searchTerm(raw string) (text, kind string, id int64, err error) {
	text = strings.TrimSpace(raw)
	if len(text) > 120 {
		return "", "", 0, ErrInvalidSearch
	}
	if m := idSearch.FindStringSubmatch(text); m != nil {
		id, err = strconv.ParseInt(m[2], 10, 64)
		kind = strings.ToLower(m[1])
		if err != nil || id < 1 {
			return "", "", 0, ErrInvalidSearch
		}
	}
	return
}
func searchPattern(text string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(text) + "%"
}

// inputSearch is shared by pagination's count and page query.
func inputSearch(raw string) (string, []any, error) {
	text, kind, id, e := searchTerm(raw)
	if e != nil {
		return "", nil, e
	}
	if kind == "account" {
		return " WHERE a.id=?", []any{id}, nil
	}
	if kind == "stream" {
		return " WHERE EXISTS(SELECT 1 FROM streams s WHERE s.account_id=a.id AND s.id=?)", []any{id}, nil
	}
	pattern := searchPattern(text)
	return ` WHERE a.provider LIKE ? ESCAPE '\' OR a.state LIKE ? ESCAPE '\' OR cast(a.id AS TEXT)=? OR EXISTS(SELECT 1 FROM streams s WHERE s.account_id=a.id AND cast(s.id AS TEXT)=?)`, []any{pattern, pattern, text, text}, nil
}

type StreamMatch struct {
	ID        int64    `json:"stream_id"`
	AccountID int64    `json:"account_id"`
	Name      string   `json:"name"`
	Provider  string   `json:"provider"`
	State     string   `json:"state"`
	InRoster  bool     `json:"in_roster"`
	Channels  []string `json:"channels"`
}
type OutputSearch struct {
	Channels  []string      `json:"channels"`
	Streams   []StreamMatch `json:"streams"`
	Truncated bool          `json:"truncated"`
	ExactKind string        `json:"exact_kind,omitempty"`
	Exists    bool          `json:"exists"`
}

// SearchOutput searches redacted metadata in one read snapshot, including queued
// and excluded streams. It never fetches or probes a source, and caps detail rows.
func (s *Store) SearchOutput(raw string) (OutputSearch, error) {
	out := OutputSearch{Channels: []string{}, Streams: []StreamMatch{}}
	text, kind, id, e := searchTerm(raw)
	if e != nil {
		return out, e
	}
	out.ExactKind = kind
	tx, e := s.DB.Begin()
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if kind != "" {
		query := "SELECT EXISTS(SELECT 1 FROM accounts WHERE id=?)"
		if kind == "stream" {
			query = "SELECT EXISTS(SELECT 1 FROM streams WHERE id=?)"
		}
		if e = tx.QueryRow(query, id).Scan(&out.Exists); e != nil {
			return out, e
		}
	}
	predicate := `(s.name LIKE ? ESCAPE '\' OR a.provider LIKE ? ESCAPE '\' OR cast(s.id AS TEXT)=? OR cast(a.id AS TEXT)=?)`
	pattern := searchPattern(text)
	args := []any{pattern, pattern, text, text}
	if kind == "account" {
		predicate = "a.id=?"
		args = []any{id}
	}
	if kind == "stream" {
		predicate = "s.id=?"
		args = []any{id}
	}
	channelWhere := `EXISTS(SELECT 1 FROM mappings m JOIN streams s ON s.id=m.stream_id JOIN accounts a ON a.id=s.account_id WHERE m.channel_id=c.id AND ` + predicate + `)`
	channelArgs := append([]any{}, args...)
	if kind == "" {
		channelWhere = `(c.name LIKE ? ESCAPE '\' OR c.number=? OR ` + channelWhere + `)`
		channelArgs = append([]any{pattern, text}, channelArgs...)
	}
	rows, e := tx.Query("SELECT c.id FROM channels c WHERE "+channelWhere+" ORDER BY CAST(c.number AS REAL),c.id", channelArgs...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var ch string
		if e = rows.Scan(&ch); e != nil {
			rows.Close()
			return out, e
		}
		out.Channels = append(out.Channels, ch)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	rows, e = tx.Query(`SELECT s.id,s.account_id,s.name,a.provider,s.state,EXISTS(SELECT 1 FROM roster r WHERE r.stream_id=s.id),coalesce((SELECT group_concat(m.channel_id) FROM mappings m WHERE m.stream_id=s.id),'') FROM streams s JOIN accounts a ON a.id=s.account_id WHERE `+predicate+` ORDER BY s.id LIMIT 101`, args...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v StreamMatch
		var channels string
		if e = rows.Scan(&v.ID, &v.AccountID, &v.Name, &v.Provider, &v.State, &v.InRoster, &channels); e != nil {
			rows.Close()
			return out, e
		}
		v.Channels = []string{}
		if channels != "" {
			v.Channels = strings.Split(channels, ",")
		}
		out.Streams = append(out.Streams, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(out.Streams) > 100 {
		out.Truncated = true
		out.Streams = out.Streams[:100]
	}
	return out, tx.Commit()
}
