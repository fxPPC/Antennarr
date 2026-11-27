package guide

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"antennarr/internal/budget"
	"antennarr/internal/catalog"
	"antennarr/internal/netguard"
)

type Service struct {
	store  *catalog.Store
	client *http.Client
	Budget *budget.Budget
	wake   chan struct{}
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func New(store *catalog.Store, client *http.Client) (*Service, error) {
	if store == nil || client == nil {
		return nil, errors.New("guide requires catalogue and HTTP client")
	}
	return &Service{store: store, client: client, wake: make(chan struct{}, 1)}, nil
}
func (s *Service) Trigger() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(15 * time.Minute)
	defer tick.Stop()
	s.Trigger()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-tick.C:
		}
		_ = s.Refresh(ctx)
	}
}

// Live playback can cancel and reclaim a background metadata reservation.
func (s *Service) Preempt() {
	s.mu.Lock()
	done := s.done
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

type snapshot struct {
	Schedule   []Programme `json:"schedule"`
	Candidates []Candidate `json:"candidates"`
	Conflict   bool        `json:"conflict"`
	Checked    int64       `json:"checked"`
}
type job struct {
	url, origin, budgetProvider string
	account                     int64
	limit                       int
	profiles                    map[string]bool
	ids                         map[string]map[string]bool
	names                       map[string]map[string]bool
}
type channelInput struct {
	ch  catalog.Channel
	pin string
	old snapshot
}
type inputs struct {
	channels map[string]*channelInput
	feeds    map[string]string
	jobs     []*job
	labels   map[string]map[string][]string
	pins     map[string][]string
}

func (s *Service) inputs(ctx context.Context) (inputs, error) {
	in := inputs{channels: map[string]*channelInput{}, feeds: map[string]string{}, labels: map[string]map[string][]string{}, pins: map[string][]string{}}
	channels, e := s.store.ActiveChannels()
	if e != nil {
		return in, e
	}
	for _, ch := range channels {
		if ch.Enabled {
			in.channels[ch.ID] = &channelInput{ch: ch}
			idx := in.labels[ch.ProfileID]
			if idx == nil {
				idx = map[string][]string{}
				in.labels[ch.ProfileID] = idx
			}
			for _, name := range append([]string{ch.Station, ch.Name}, ch.Aliases...) {
				name = norm(name)
				if name != "" && !strings.HasPrefix(name, "-") && name != "@guide" {
					idx[name] = append(idx[name], ch.ID)
				}
			}
		}
	}
	rows, e := s.store.DB.QueryContext(ctx, "SELECT g.channel_id,g.pin,g.snapshot FROM guide_channels g JOIN active_channels c ON c.id=g.channel_id WHERE c.enabled=1")
	if e != nil {
		return in, e
	}
	for rows.Next() {
		var id, pin, data string
		if e = rows.Scan(&id, &pin, &data); e != nil {
			rows.Close()
			return in, e
		}
		if c := in.channels[id]; c != nil {
			c.pin = pin
			if pin != "" {
				in.pins[pin] = append(in.pins[pin], id)
			}
			if e = json.Unmarshal([]byte(data), &c.old); e != nil {
				rows.Close()
				return in, e
			}
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return in, e
	}
	rows, e = s.store.DB.QueryContext(ctx, "SELECT f.profile_id,f.url FROM guide_feeds f JOIN output_profiles p ON p.id=f.profile_id WHERE p.enabled=1 ORDER BY f.profile_id")
	if e != nil {
		return in, e
	}
	byURL := map[string]*job{}
	for rows.Next() {
		var profile, raw string
		if e = rows.Scan(&profile, &raw); e != nil {
			rows.Close()
			return in, e
		}
		in.feeds[profile] = raw
		j := byURL[raw]
		if j == nil {
			u, _ := url.Parse(raw)
			j = &job{url: raw, origin: catalog.ProviderKey(u.Host), profiles: map[string]bool{}}
			in.jobs = append(in.jobs, j)
			byURL[raw] = j
		}
		j.profiles[profile] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return in, e
	}
	roster, e := s.store.GuideRosters(ctx)
	if e != nil {
		return in, e
	}
	// A feed explicitly configured on two tuners, an operator-listed portal
	// guide and a user-supplied feed can all refer to the very same URL.
	providers := map[string]*job{}
	portalURLs := map[int64]string{}
	for _, r := range roster {
		c := in.channels[r.ChannelID]
		if c == nil {
			continue
		}
		host := catalog.ProviderKey(r.Provider)
		var j *job
		var raw string
		switch r.Kind {
		case "xtream":
			if r.Station == "" {
				continue
			}
			j = providers[host]
			if j == nil {
				sec, err := s.store.AccountSecret(r.Account)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						continue
					}
					return in, err
				}
				u, err := url.Parse(strings.TrimRight(sec.Endpoint, "/") + "/xmltv.php")
				if err != nil || netguard.URL(u) != nil {
					continue
				}
				q := u.Query()
				q.Set("username", sec.Username)
				q.Set("password", sec.Password)
				u.RawQuery = q.Encode()
				raw = u.String()
			}
		case "portal":
			var ok bool
			raw, ok = portalURLs[r.Account]
			if !ok {
				cfg, err := s.store.PortalAccount(r.Account)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						continue
					}
					return in, err
				}
				raw = cfg.XMLTV()
				portalURLs[r.Account] = raw
			}
			if raw == "" {
				continue
			}
		default:
			continue
		}
		if j == nil {
			j = byURL[raw]
			if j == nil {
				u, err := url.Parse(raw)
				if err != nil || netguard.URL(u) != nil {
					continue
				}
				j = &job{url: raw, origin: catalog.ProviderKey(u.Host)}
				in.jobs = append(in.jobs, j)
				byURL[raw] = j
			}
			if j.account == 0 {
				j.account = r.Account
				j.limit = r.Limit
				j.budgetProvider = host
			}
			if r.Kind == "xtream" {
				providers[host] = j
			}
		}
		if j.ids == nil {
			j.ids = map[string]map[string]bool{}
			j.names = map[string]map[string]bool{}
		}
		if r.Station != "" {
			addTarget(j.ids, r.Station, r.ChannelID)
		}
		if r.Kind == "portal" {
			for _, name := range append([]string{c.ch.Name, c.ch.Station}, c.ch.Aliases...) {
				if name != "" && !strings.HasPrefix(name, "-") && name != "@guide" {
					addTarget(j.names, norm(name), r.ChannelID)
				}
			}
		}
	}

	return in, nil
}
func addTarget(index map[string]map[string]bool, key, id string) {
	if index[key] == nil {
		index[key] = map[string]bool{}
	}
	index[key][id] = true
}
func norm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func (s *Service) fetch(ctx context.Context, j *job, now int64) (map[string]*station, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	if s.Budget != nil {
		account, limit := j.account, j.limit
		if account == 0 {
			account = -1
			limit = 1
		}
		provider := j.budgetProvider
		if provider == "" {
			provider = j.origin
		}
		release, ok := s.Budget.ReserveGuide(account, provider, limit)
		if !ok {
			return nil, "", errors.New("guide deferred")
		}
		defer release()
	}
	resp, e := netguard.Request(ctx, s.client, http.MethodGet, j.url)
	if e != nil {
		return nil, "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", errors.New("guide source unavailable")
	}
	origin := j.origin
	if resp.Request != nil && resp.Request.URL != nil {
		origin = catalog.ProviderKey(resp.Request.URL.Host)
	}
	stations, e := parseSource(contextReader{ctx, resp.Body}, now, &origin)
	return stations, origin, e
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.reader.Read(p)
}

type inventory map[string]map[string]Candidate

func (s *Service) Refresh(parent context.Context) error {
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		return errors.New("guide refresh already running")
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		cancel()
		close(s.done)
		s.done = nil
		s.cancel = nil
		s.mu.Unlock()
	}()
	now := time.Now().Unix()
	in, e := s.inputs(ctx)
	if e != nil {
		return e
	}
	choices := map[string]map[string]Candidate{}
	list := inventory{}
	// Guide feeds with many ambiguously named stations can otherwise make a
	// single origin dominate comparison work. A pin remains selectable from the
	// external station index, regardless of the number of competing matches.
	type originKey struct{ channel, origin string }
	perOrigin := map[originKey][]string{}
	ambiguous := map[originKey]bool{}
	for _, j := range in.jobs {
		if e := ctx.Err(); e != nil {
			return e
		}
		stations, origin, err := s.fetch(ctx, j, now)
		if err != nil {
			continue
		}
		// A failed external refresh retains its searchable inventory. A successful
		// replacement updates all rows in the same transaction as its schedule.
		for p := range j.profiles {
			list[p] = map[string]Candidate{}
		}
		for id, st := range stations {
			if err := ctx.Err(); err != nil {
				return err
			}
			sid := stationID(origin, id)
			n, next := nowNext(st.programmes, now)
			candidate := Candidate{ID: sid, Source: origin, Station: id, Name: st.displayName(), Now: n, Next: next, programmes: st.programmes}
			targets := map[string]bool{}
			for c := range j.ids[id] {
				targets[c] = true
			}
			for p := range j.profiles {
				sample := candidate
				sample.programmes = nil
				list[p][sid] = sample
				for _, name := range append([]string{id}, st.names...) {
					for _, c := range in.labels[p][norm(name)] {
						targets[c] = true
					}
				}
			}
			// A pin carries its station through account/roster changes and may select
			// any station in the user's external XMLTV feed.
			for _, name := range append([]string{id}, st.names...) {
				for ch := range j.names[norm(name)] {
					targets[ch] = true
				}
			}
			for _, ch := range in.pins[sid] {
				if j.profiles[in.channels[ch].ch.ProfileID] || j.ids[id][ch] {
					targets[ch] = true
				}
			}
			for ch := range targets {
				c := in.channels[ch]
				if c == nil || (c.pin != sid && !catalog.GuideAllowed(c.ch, id, candidate.Name)) {
					continue
				}
				if choices[ch] == nil {
					choices[ch] = map[string]Candidate{}
				}
				if _, seen := choices[ch][sid]; seen {
					continue
				}
				key := originKey{ch, origin}
				if !ambiguous[key] {
					group := perOrigin[key]
					if len(group) < 8 {
						perOrigin[key] = append(group, sid)
					} else {
						ambiguous[key] = true
						for _, other := range group {
							if other != c.pin {
								delete(choices[ch], other)
							}
						}
						delete(perOrigin, key)
					}
				}
				if !ambiguous[key] || c.pin == sid {
					choices[ch][sid] = candidate
				}
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.publish(ctx, in, choices, list, now)
}

func (s *Service) publish(ctx context.Context, in inputs, choices map[string]map[string]Candidate, list inventory, now int64) error {
	// Calculate and serialise consensus outside the SQLite write transaction.
	// Publishing below then swaps the entire completed generation atomically.
	updated := map[string][]byte{}
	for id, c := range in.channels {
		if err := ctx.Err(); err != nil {
			return err
		}
		candidates := make([]Candidate, 0, len(choices[id]))
		for _, v := range choices[id] {
			candidates = append(candidates, v)
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
		resolved, conflict := resolveContext(ctx, candidates, c.old.Schedule, c.pin, now)
		if len(resolved) == 0 && len(candidates) == 0 && len(c.old.Schedule) == 0 && len(c.old.Candidates) == 0 {
			continue
		}
		data, err := json.Marshal(snapshot{Schedule: resolved, Candidates: candidates, Conflict: conflict, Checked: now})
		if err != nil {
			return err
		}
		updated[id] = data
	}
	tx, e := s.store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	valid := map[string]bool{}
	for _, c := range in.channels {
		p := c.ch.ProfileID
		if _, ok := valid[p]; !ok {
			var current string
			err := tx.QueryRowContext(ctx, "SELECT url FROM guide_feeds WHERE profile_id=?", p).Scan(&current)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			valid[p] = current == in.feeds[p]
		}
	}
	for id, data := range updated {
		c := in.channels[id]
		if !valid[c.ch.ProfileID] {
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO guide_channels(channel_id,pin,snapshot) SELECT id,?,? FROM channels WHERE id=? AND profile_id=? ON CONFLICT(channel_id) DO UPDATE SET snapshot=excluded.snapshot WHERE guide_channels.pin=excluded.pin`, c.pin, string(data), id, c.ch.ProfileID)
		if err != nil {
			return err
		}
	}
	for profile, stations := range list {
		if !valid[profile] {
			continue
		}
		if _, e = tx.ExecContext(ctx, "DELETE FROM guide_stations WHERE profile_id=?", profile); e != nil {
			return e
		}
		for id, v := range stations {
			data, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO guide_stations(profile_id,id,station,name,sample) SELECT id,?,?,?,? FROM output_profiles WHERE id=?`, id, v.Station, v.Name, string(data), profile); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}
