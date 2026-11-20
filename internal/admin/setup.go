package admin

import (
	"antennarr/internal/catalog"
	"errors"
	"net/http"
)

func (a *API) setup(w http.ResponseWriter, r *http.Request) {
	p, err := a.Store.SetupProgress()
	if err != nil {
		failure(w, 500, "Setup progress unavailable")
		return
	}
	response(w, p)
}

func (a *API) saveSetup(w http.ResponseWriter, r *http.Request) {
	var update catalog.SetupUpdate
	if !decode(w, r, &update) {
		return
	}
	p, err := a.Store.UpdateSetup(update)
	switch {
	case errors.Is(err, catalog.ErrSetupChoice):
		failure(w, 400, err.Error())
	case errors.Is(err, catalog.ErrSetupProfile), errors.Is(err, catalog.ErrSetupIncomplete):
		failure(w, 409, err.Error())
	case err != nil:
		failure(w, 500, "Could not save setup progress")
	default:
		response(w, p)
	}
}
