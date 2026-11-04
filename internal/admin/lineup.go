package admin

import (
	"antennarr/internal/catalog"
	"encoding/json"
	"io"
	"net/http"
)

func (a *API) exportLineup(w http.ResponseWriter, r *http.Request) {
	v, err := a.Store.ExportLineup()
	if err != nil {
		failure(w, 500, "Could not read channel lineup")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="antennarr-channels.json"`)
	response(w, v)
}
func (a *API) importLineup(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Confirmed bool           `json:"confirmed"`
		Lineup    catalog.Lineup `json:"lineup"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		failure(w, 400, "Invalid channel file (limit 2 MiB)")
		return
	}
	if d.Decode(new(any)) != io.EOF || !v.Confirmed || v.Lineup.Validate() != nil {
		failure(w, 400, "Invalid or unconfirmed channel replacement")
		return
	}
	if a.Idle == nil || !a.Idle() {
		failure(w, 409, "Stop playback before replacing the lineup")
		return
	}
	apply := func() error { return a.Store.ImportLineup(v.Lineup) }
	if a.WithIdle != nil {
		original := apply
		apply = func() error { return a.WithIdle(original) }
	}
	var err error
	if a.WithChecksPaused != nil {
		err = a.WithChecksPaused(apply)
	} else {
		err = apply()
	}
	if err != nil {
		failure(w, 500, "Lineup replacement failed; saved channels retained")
		return
	}
	response(w, map[string]bool{"saved": true})
}
