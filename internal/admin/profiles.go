package admin

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"net"
	"net/http"
	"strconv"
)

func requestProfile(r *http.Request) string {
	if id := r.URL.Query().Get("profile_id"); id != "" {
		return id
	}
	return "primary"
}

func (a *API) ports() config.TunerPorts {
	p := a.TunerPorts
	if p.First == 0 {
		p, _ = config.ParseTunerPorts("")
	}
	return p
}

type profileView struct {
	catalog.Profile
	TunerURL     string `json:"tuner_url"`
	ListenPort   int    `json:"listen_port"`
	ChannelCount int    `json:"channel_count"`
	AddressError string `json:"address_error,omitempty"`
}

func (a *API) profiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := a.Store.Profiles()
	if err != nil {
		failure(w, 500, "Tuners unavailable")
		return
	}
	views := make([]profileView, 0, len(profiles))
	for _, p := range profiles {
		v := profileView{Profile: p}
		v.TunerURL, err = a.ports().URL(a.BaseURL, p.PortOffset)
		if err != nil {
			v.AddressError = err.Error()
		}
		if p.PortOffset == 0 {
			v.ListenPort = 9192
			if a.PrimaryListen != "" {
				_, raw, _ := net.SplitHostPort(a.PrimaryListen)
				v.ListenPort, _ = strconv.Atoi(raw)
			}
		} else {
			v.ListenPort, _ = a.ports().Port(p.PortOffset)
		}
		channels, e := a.Store.ChannelsForProfile(p.ID)
		if e != nil {
			failure(w, 500, "Tuner channels unavailable")
			return
		}
		v.ChannelCount = len(channels)
		views = append(views, v)
	}
	response(w, views)
}

func (a *API) presets(w http.ResponseWriter, r *http.Request) { response(w, catalog.Presets()) }

func (a *API) addProfile(w http.ResponseWriter, r *http.Request) {
	var v struct {
		PresetID string `json:"preset_id"`
	}
	if !decode(w, r, &v) {
		return
	}
	p := a.ports()
	var out catalog.Profile
	apply := func() error {
		var err error
		out, err = a.Store.AddProfilePreset(v.PresetID, p.Last-p.First+1)
		return err
	}
	var err error
	if a.WithChecksPaused != nil {
		err = a.WithChecksPaused(apply)
	} else {
		err = apply()
	}
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	response(w, out)
}

func (a *API) saveProfile(w http.ResponseWriter, r *http.Request) {
	// Partial updates prevent an enable checkbox from clearing market metadata.
	var v struct {
		Name          *string `json:"name"`
		Enabled       *bool   `json:"enabled"`
		Country       *string `json:"country"`
		Market        *string `json:"market"`
		Timezone      *string `json:"timezone"`
		GuideLocation *string `json:"guide_location"`
	}
	if !decode(w, r, &v) {
		return
	}
	p, err := a.Store.Profile(r.PathValue("id"))
	if err != nil {
		failure(w, 404, "Tuner not found")
		return
	}
	if v.Name != nil {
		p.Name = *v.Name
	}
	if v.Enabled != nil {
		p.Enabled = *v.Enabled
	}
	if v.Country != nil {
		p.Country = *v.Country
	}
	if v.Market != nil {
		p.Market = *v.Market
	}
	if v.Timezone != nil {
		p.Timezone = *v.Timezone
	}
	if v.GuideLocation != nil {
		p.GuideLocation = *v.GuideLocation
	}
	if p.Enabled {
		if _, err = a.ports().URL(a.BaseURL, p.PortOffset); err != nil {
			failure(w, 400, err.Error())
			return
		}
	}
	apply := func() error { p, err = a.Store.SaveProfile(p); return err }
	if a.WithChecksPaused != nil {
		err = a.WithChecksPaused(apply)
	} else {
		err = apply()
	}
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	response(w, p)
}

func (a *API) deleteProfile(w http.ResponseWriter, r *http.Request) {
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
	if a.Idle == nil || !a.Idle() {
		failure(w, 409, "Stop playback before removing a tuner")
		return
	}
	apply := func() error { return a.Store.DeleteProfile(r.PathValue("id")) }
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
		failure(w, 500, "Tuner removal failed; saved channels retained")
		return
	}
	response(w, map[string]bool{"saved": true})
}
