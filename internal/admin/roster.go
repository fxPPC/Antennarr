package admin

import (
	"antennarr/internal/catalog"
	"errors"
	"net/http"
	"strconv"
)

func (a *API) rosterSummary(w http.ResponseWriter, r *http.Request) {
	var v catalog.RosterSummary
	var e error
	if r.URL.Query().Get("queue") == "1" {
		v, e = a.Store.RosterSummary()
	} else {
		v, e = a.Store.RosterState()
	}
	if e != nil {
		failure(w, 500, "Roster unavailable")
		return
	}
	response(w, v)
}
func (a *API) rosterEntries(w http.ResponseWriter, r *http.Request) {
	v, e := a.Store.RosterEntries(r.PathValue("id"))
	if e != nil {
		failure(w, 500, "Roster unavailable")
		return
	}
	response(w, v)
}
func (a *API) rosterPolicy(w http.ResponseWriter, r *http.Request) {
	var p catalog.RosterPolicy
	if !decode(w, r, &p) {
		return
	}
	if a.Store.SaveChannelPolicy(r.PathValue("id"), p) != nil {
		failure(w, 400, "Invalid roster policy")
		return
	}
	response(w, map[string]bool{"saved": true})
}
func (a *API) inputAccounts(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	v, e := a.Store.InputPage(r.URL.Query().Get("q"), offset)
	if errors.Is(e, catalog.ErrInvalidSearch) {
		failure(w, 400, "Invalid search; use a positive account/stream ID or up to 120 characters")
		return
	}
	if e != nil {
		failure(w, 500, "Queue unavailable")
		return
	}
	response(w, v)
}

func (a *API) importStatus(w http.ResponseWriter, r *http.Request) {
	var id, started, finished int64
	var n int
	var state, summary string
	e := a.Store.DB.QueryRow("SELECT id,started,finished,state,processed,summary FROM import_jobs ORDER BY id DESC LIMIT 1").Scan(&id, &started, &finished, &state, &n, &summary)
	if e != nil {
		response(w, map[string]string{"state": "idle"})
		return
	}
	if state == "running" && !a.importing.Load() {
		state = "interrupted"
		summary = "Import interrupted by restart; submit the list again. Existing entries are retained."
	}
	response(w, map[string]any{"job_id": id, "started": started, "finished": finished, "state": state, "processed": n, "summary": summary})
}
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	v, e := a.Store.RecentRosterEvents()
	if e != nil {
		failure(w, 500, "Events unavailable")
		return
	}
	response(w, v)
}

func (a *API) clearQueue(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Confirmed bool `json:"confirmed"`
	}
	if !decode(w, r, &v) {
		return
	}
	if !v.Confirmed {
		failure(w, 400, "Confirm queue removal first")
		return
	}
	if a.WithChecksPaused == nil || a.WithProtectedAccounts == nil || a.DataRoot == "" {
		failure(w, 503, "Queue clearing unavailable")
		return
	}
	var removed catalog.QueueClear
	e := a.WithChecksPaused(func() error {
		var e error
		return a.WithProtectedAccounts(func(ids []int64) error { removed, e = a.Store.ClearQueue(ids); return e })
	})
	if e != nil {
		failure(w, 500, "Could not clear queue; existing rosters are preserved")
		return
	}
	response(w, map[string]any{"removed": removed})
}

func (a *API) excludedStreams(w http.ResponseWriter, r *http.Request) {
	v, e := a.Store.ExcludedStreams(r.PathValue("id"))
	if e != nil {
		failure(w, 500, "Excluded streams unavailable")
		return
	}
	response(w, v)
}

func (a *API) outputSearch(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query().Get("q")) > 120 {
		failure(w, 400, "Search is too long")
		return
	}
	v, e := a.Store.SearchOutput(r.URL.Query().Get("q"))
	if errors.Is(e, catalog.ErrInvalidSearch) {
		failure(w, 400, "Invalid search ID")
		return
	}
	if e != nil {
		failure(w, 500, "Search unavailable")
		return
	}
	response(w, v)
}
