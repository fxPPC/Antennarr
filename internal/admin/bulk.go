package admin

import (
	"antennarr/internal/recovery"
	"net/http"
)

func (a *API) bulkChannels(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Confirmed   bool `json:"confirmed"`
		DefaultUS   bool `json:"default_us"`
		ResetPreset bool `json:"reset_preset"`
	}
	if !decode(w, r, &v) {
		return
	}
	if !v.Confirmed {
		failure(w, 400, "Confirmation required")
		return
	}
	if a.Idle == nil || !a.Idle() {
		failure(w, 409, "Stop viewing before changing all channels")
		return
	}
	profile, err := a.Store.Profile(requestProfile(r))
	if err != nil {
		failure(w, 404, "Tuner not found")
		return
	}
	preset := ""
	if v.ResetPreset {
		preset = profile.Preset
	}
	if v.DefaultUS {
		if profile.ID != "primary" {
			failure(w, 400, "Use this tuner’s own preset")
			return
		}
		preset = "us-nyc"
	}
	if v.ResetPreset && preset == "" {
		failure(w, 400, "This tuner has no preset")
		return
	}
	apply := func() error { return a.Store.ReplaceProfileChannels(profile.ID, preset) }
	if a.WithIdle != nil {
		original := apply
		apply = func() error { return a.WithIdle(original) }
	}
	var e error
	if a.WithChecksPaused != nil {
		e = a.WithChecksPaused(apply)
	} else {
		e = apply()
	}
	if e != nil {
		failure(w, 500, "Channel operation failed; saved channels retained")
		return
	}
	response(w, map[string]bool{"saved": true})
}
func (a *API) resetApplication(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Confirmed bool `json:"confirmed"`
	}
	if !decode(w, r, &v) {
		return
	}
	if !v.Confirmed {
		failure(w, 400, "Confirmation required")
		return
	}
	if a.Idle == nil || a.Restart == nil || a.DataRoot == "" {
		failure(w, 503, "Reset unavailable")
		return
	}
	if !a.Idle() {
		failure(w, 409, "Stop viewing before resetting")
		return
	}
	if recovery.StageReset(a.DataRoot) != nil {
		failure(w, 500, "Could not stage reset")
		return
	}
	a.maintenance.Store(true)
	response(w, map[string]bool{"resetting": true})
	a.Restart()
}
