package admin

import (
	"context"
	"net/http"
	"net/url"
	"sync"

	"antennarr/internal/catalog"
	"antennarr/internal/ingest"
)

// PortalWork allows a tune request to synchronously reclaim a manual refresh's
// upstream slot. SQLite updates run only after the network reservation is released.
type PortalWork struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func (w *PortalWork) Preempt() {
	if w == nil {
		return
	}
	w.mu.Lock()
	done := w.done
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (w *PortalWork) start(ctx context.Context) (context.Context, func()) {
	if w == nil {
		return ctx, func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	w.mu.Lock()
	w.cancel, w.done = cancel, done
	w.mu.Unlock()
	return ctx, func() {
		cancel()
		w.mu.Lock()
		w.cancel, w.done = nil, nil
		close(done)
		w.mu.Unlock()
	}
}

func (a *API) portal(w http.ResponseWriter, r *http.Request) {
	var cfg ingest.PortalAccount
	if !decode(w, r, &cfg) {
		return
	}
	if err := cfg.Normalise(); err != nil {
		failure(w, 400, err.Error())
		return
	}
	id, err := a.Store.PortalAccountID(cfg)
	if err != nil {
		failure(w, 500, "Cannot read portal account")
		return
	}
	a.importPortal(w, r, id, cfg)
}

func (a *API) refreshPortal(w http.ResponseWriter, r *http.Request) {
	var v struct {
		AccountID int64 `json:"account_id"`
	}
	if !decode(w, r, &v) {
		return
	}
	cfg, err := a.Store.PortalAccount(v.AccountID)
	if err != nil {
		failure(w, 404, "Portal account unavailable")
		return
	}
	a.importPortal(w, r, v.AccountID, cfg)
}

func (a *API) importPortal(w http.ResponseWriter, r *http.Request, id int64, cfg ingest.PortalAccount) {
	if !a.importing.CompareAndSwap(false, true) {
		failure(w, 409, "Another source import is active")
		return
	}
	defer a.importing.Store(false)
	u, _ := url.Parse(cfg.PortalURL)
	if a.PortalBudget == nil || a.Client == nil {
		failure(w, 503, "Portal service unavailable")
		return
	}
	ctx, finish := a.PortalWork.start(r.Context())
	release, ok := a.PortalBudget(id, catalog.ProviderKey(u.Host), 1, false)
	if !ok {
		finish()
		failure(w, 409, "Portal refresh deferred while connections are in use")
		return
	}
	channels, err := cfg.Channels(ctx, a.Client)
	release()
	wasCancelled := ctx.Err() != nil
	finish()
	if wasCancelled {
		failure(w, 409, "Portal refresh interrupted by playback or cancellation")
		return
	}
	if err != nil {
		failure(w, 502, err.Error())
		return
	}
	id, err = a.Store.SavePortalCatalogue(cfg, channels)
	if err != nil {
		failure(w, 500, "Could not save portal catalogue")
		return
	}
	response(w, map[string]any{"account_id": id, "channels": len(channels)})
}
