package tuner

import (
	"antennarr/internal/catalog"
	"antennarr/internal/config"
	"net/http"
	"strings"
)

// Fleet routes one private listener to one saved profile. All devices reuse
// the template's stream manager, so limits are global and channel IDs (never
// overlapping guide numbers) are the upstream sharing key.
type Fleet struct {
	Store    *catalog.Store
	Template *Server
	Ports    config.TunerPorts
}

func (f *Fleet) Handler(offset int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		profiles, err := f.Store.Profiles()
		if err != nil {
			http.Error(w, "Tuner configuration unavailable", 503)
			return
		}
		var profile *catalog.Profile
		for i := range profiles {
			if profiles[i].PortOffset == offset {
				profile = &profiles[i]
				break
			}
		}
		if profile == nil {
			http.NotFound(w, r)
			return
		}
		if !profile.Enabled {
			http.Error(w, "Tuner disabled", 503)
			return
		}
		base, err := f.Ports.URL(f.Template.BaseURL, offset)
		if err != nil {
			http.Error(w, "Tuner address unavailable", 503)
			return
		}
		s := *f.Template
		s.BaseURL = base
		s.FriendlyName = profile.Name
		if profile.DeviceID != "" {
			s.Config.DeviceID = profile.DeviceID
		}
		selected, err := f.Store.ChannelsForProfile(profile.ID)
		if err != nil {
			http.Error(w, "Tuner lineup unavailable", 503)
			return
		}
		channels := make([]config.Channel, 0, len(selected))
		for _, ch := range selected {
			if ch.Enabled {
				channels = append(channels, config.Channel{ID: ch.ID, Number: ch.Number, Name: ch.Name})
			}
		}
		s.ChannelList = func() []config.Channel { return channels }
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		s.TunerHandler().ServeHTTP(w, r)
	})
}

func tunerPath(path string) bool {
	switch path {
	case "/discover.json", "/lineup.json", "/lineup_status.json", "/lineup.post", "/device.xml":
		return true
	}
	return strings.HasPrefix(path, "/auto/")
}

// PrimaryHandler preserves the original device address and legacy protected
// admin routes, even if the primary tuner is disabled or explicitly removed.
func (f *Fleet) PrimaryHandler() http.Handler {
	device, administration := f.Handler(0), f.Template.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tunerPath(r.URL.Path) {
			device.ServeHTTP(w, r)
			return
		}
		administration.ServeHTTP(w, r)
	})
}

// FallbackHandler retains the legacy primary-device-only 5004 endpoint. A
// device without a port cannot disambiguate overlapping market guide numbers.
func (f *Fleet) FallbackHandler() http.Handler {
	device := f.Handler(0)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/auto/") {
			http.NotFound(w, r)
			return
		}
		device.ServeHTTP(w, r)
	})
}
