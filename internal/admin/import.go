package admin

// Manual ingestion is asynchronous and isolated from playback. Source material
// remains untrusted data; only normalised entries enter the shared catalogue.
import (
	"antennarr/internal/ingest"
	"antennarr/internal/netguard"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

func (a *API) importSources(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Text       string `json:"text"`
		Provenance string `json:"provenance"`
		Authorised bool   `json:"authorised"`
	}
	if !decode(w, r, &v) {
		return
	}
	if !v.Authorised {
		failure(w, 400, "Authorisation must be confirmed")
		return
	}
	if !a.importing.CompareAndSwap(false, true) {
		failure(w, 409, "An import is already running")
		return
	}
	id, e := a.Store.StartImportJob()
	if e != nil {
		a.importing.Store(false)
		failure(w, 500, "Could not queue import")
		return
	}
	// Keep restore/backup excluded until the asynchronous import completes.
	if !a.operations.TryRLock() {
		a.importing.Store(false)
		if e := a.Store.FinishImportJob(id, "failed", 0, "Maintenance prevented the import; submit it again."); e != nil {
			slog.Warn("deferred import status could not be saved", "job_id", id)
		}
		failure(w, 409, "Maintenance is pending; try again shortly")
		return
	}
	go func() {
		defer a.operations.RUnlock()
		defer a.importing.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
		defer cancel()
		job := a.Activity.Begin("manual_import", "Reading manually submitted source pages", 0, 0)
		outcome := "failed"
		defer func() { job.Finish(outcome) }()
		client := a.Client
		if client == nil {
			client = netguard.Client(20 * time.Second)
		}
		report, err := (ingest.Resolver{Client: client}).ResolveLists(ctx, v.Text)
		n := 0
		summary := "Could not read this list. Check the URL and whether the page is public."
		if err == nil {
			job.Stage("Saving queue entries and matching selected channels")
			n, err = a.Store.Import(report.Entries, "Manual URL import")
			if err == nil {
				outcome = "imported"
				b, _ := json.Marshal(map[string]any{"pages": report.Pages, "duplicates": report.Duplicates, "warnings": report.Warnings})
				summary = string(b)
			} else {
				job.Detail("import", "local_catalogue_error", 0)
				summary = "Could not save imported entries. Check Docker logs before retrying."
				if n > 0 {
					summary = fmt.Sprintf("Saved %d entries before an import database error. Retry the list to continue; existing entries are deduplicated.", n)
				}
			}
		}
		if e := a.Store.FinishImportJob(id, outcome, n, summary); e != nil {
			slog.Warn("import completion could not be saved", "job_id", id)
		}
		if e := a.Store.PruneImportJobs(); e != nil {
			slog.Warn("import history cleanup failed")
		}
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	response(w, map[string]any{"job_id": id, "state": "running"})
}
