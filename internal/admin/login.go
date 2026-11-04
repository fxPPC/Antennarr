package admin

import (
	"antennarr/internal/config"
	"errors"
	"net/http"
)

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if a.ReadLogin == nil {
		failure(w, 503, "Login settings unavailable")
		return
	}
	response(w, map[string]string{"username": a.ReadLogin().Username})
}
func (a *API) saveLogin(w http.ResponseWriter, r *http.Request) {
	var v struct {
		CurrentPassword string `json:"current_password"`
		Username        string `json:"username"`
		Password        string `json:"password"`
	}
	if !decode(w, r, &v) {
		return
	}
	if a.ChangeLogin == nil {
		failure(w, 503, "Login settings unavailable")
		return
	}
	if e := a.ChangeLogin(v.CurrentPassword, v.Username, v.Password); e != nil {
		status := 400
		if errors.Is(e, config.ErrCurrentPassword) {
			status = 403
		}
		failure(w, status, e.Error())
		return
	}
	response(w, map[string]bool{"saved": true, "restart_required": false})
}
