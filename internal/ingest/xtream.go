package ingest

import (
	"antennarr/internal/netguard"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxCatalogueEntries bounds a provider response independently of the byte limit.
const maxCatalogueEntries = 40000

type Xtream struct{ Client *http.Client }
type AccountInfo struct {
	FailureKind       string
	Active            bool
	Expires           int64
	Limit             int
	ActiveConnections int
}

func (x Xtream) request(ctx context.Context, a Entry, action string, v any) error {
	u, e := url.Parse(a.Endpoint + "/player_api.php")
	if e != nil {
		return ProviderError{Kind: "invalid_endpoint"}
	}
	q := u.Query()
	q.Set("username", a.Username)
	q.Set("password", a.Password)
	if action != "" {
		q.Set("action", action)
	}
	u.RawQuery = q.Encode()
	r, e := netguard.Request(ctx, x.Client, "GET", u.String())
	if e != nil {
		return ProviderError{Kind: FailureKind(e), Status: FailureStatus(e), cause: e}
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		kind := "http_error"
		if r.StatusCode == 401 || r.StatusCode == 403 {
			kind = "access_denied"
		}
		if r.StatusCode == 429 {
			kind = "rate_limited"
		}
		return ProviderError{Kind: kind, Status: r.StatusCode}
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, (8<<20)+1))
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if netguard.IsTimeout(e) {
			return context.DeadlineExceeded
		}
		return ProviderError{Kind: "response_read_error"}
	}
	if len(b) > 8<<20 {
		return ProviderError{Kind: "oversized_response"}
	}
	if e = json.Unmarshal(b, v); e != nil {
		return ProviderError{Kind: "malformed_response"}
	}
	return nil
}
func numeric(v any) int64 {
	switch n := v.(type) {
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	case float64:
		// JSON numbers arrive as float64. Saturate before conversion so an
		// oversized connection count cannot wrap negative and look available.
		if math.IsNaN(n) {
			return 0
		}
		if n >= float64(math.MaxInt64) {
			return math.MaxInt64
		}
		if n <= float64(math.MinInt64) {
			return math.MinInt64
		}
		return int64(n)
	}
	return 0
}

// connectionCount keeps untrusted provider occupancy non-negative and portable
// even on a future 32-bit build. Saturation preserves the important "full" state.
func connectionCount(v any) int {
	n := numeric(v)
	if n < 0 {
		return 0
	}
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(n)
}

func (x Xtream) Validate(ctx context.Context, a Entry) (AccountInfo, error) {
	var r struct {
		User map[string]any `json:"user_info"`
	}
	if e := x.request(ctx, a, "", &r); e != nil {
		return AccountInfo{}, e
	}
	if r.User == nil {
		return AccountInfo{}, ProviderError{Kind: "malformed_response"}
	}
	limit := connectionCount(r.User["max_connections"])
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}
	expires := numeric(r.User["exp_date"])
	active := numeric(r.User["auth"]) == 1 && strings.EqualFold(fmtString(r.User["status"]), "Active") && (expires == 0 || expires > time.Now().Unix())
	reason := ""
	if !active {
		reason = "inactive_account"
		if numeric(r.User["auth"]) != 1 {
			reason = "invalid_credentials"
		}
		if (expires > 0 && expires <= time.Now().Unix()) || strings.EqualFold(fmtString(r.User["status"]), "Expired") {
			reason = "expired_account"
		}
	}
	return AccountInfo{FailureKind: reason, Active: active, Expires: expires, Limit: limit, ActiveConnections: connectionCount(r.User["active_cons"])}, nil
}
func fmtString(v any) string { s, _ := v.(string); return s }
func (x Xtream) Live(ctx context.Context, a Entry) ([]Entry, error) {
	var rows []struct {
		Name string      `json:"name"`
		ID   json.Number `json:"stream_id"`
		EPG  string      `json:"epg_channel_id"`
	}
	if e := x.request(ctx, a, "get_live_streams", &rows); e != nil {
		return nil, e
	}
	if len(rows) > maxCatalogueEntries {
		return nil, ProviderError{Kind: "oversized_catalogue"}
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		if _, e := strconv.ParseInt(string(r.ID), 10, 64); e != nil {
			continue
		}
		v := a
		v.Kind = "xtream"
		v.Name = r.Name
		v.TVGID = r.EPG
		v.URL = a.Endpoint + "/live/" + url.PathEscape(a.Username) + "/" + url.PathEscape(a.Password) + "/" + string(r.ID) + ".ts"
		out = append(out, v)
	}
	return out, nil
}
