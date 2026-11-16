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
const maxCatalogueBytes = 64 << 20

type Xtream struct{ Client *http.Client }
type AccountInfo struct {
	FailureKind       string
	Active            bool
	Expires           int64
	Limit             int
	ActiveConnections int
}

func (x Xtream) response(ctx context.Context, a Entry, action string) (*http.Response, error) {
	u, e := url.Parse(a.Endpoint + "/player_api.php")
	if e != nil {
		return nil, ProviderError{Kind: "invalid_endpoint"}
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
		return nil, ProviderError{Kind: FailureKind(e), Status: FailureStatus(e), cause: e}
	}
	if r.StatusCode != 200 {
		r.Body.Close()
		kind := "http_error"
		if r.StatusCode == 401 || r.StatusCode == 403 {
			kind = "access_denied"
		}
		if r.StatusCode == 429 {
			kind = "rate_limited"
		}
		return nil, ProviderError{Kind: kind, Status: r.StatusCode}
	}
	return r, nil
}

func (x Xtream) request(ctx context.Context, a Entry, action string, v any) error {
	r, e := x.response(ctx, a, action)
	if e != nil {
		return e
	}
	defer r.Body.Close()
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
	r, err := x.response(ctx, a, "get_live_streams")
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.ContentLength > maxCatalogueBytes {
		return nil, ProviderError{Kind: "oversized_response"}
	}
	// Decode one entry at a time: provider logos and unused metadata must not
	// require a second, full-catalogue buffer. Both wire bytes and rows are bounded.
	limited := &io.LimitedReader{R: r.Body, N: maxCatalogueBytes + 1}
	decoder := json.NewDecoder(limited)
	readError := func(err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if netguard.IsTimeout(err) {
			return context.DeadlineExceeded
		}
		if limited.N == 0 {
			return ProviderError{Kind: "oversized_response"}
		}
		return ProviderError{Kind: "malformed_response"}
	}
	opening, err := decoder.Token()
	if err != nil {
		return nil, readError(err)
	}
	if opening != json.Delim('[') {
		return nil, ProviderError{Kind: "malformed_response"}
	}
	out := make([]Entry, 0)
	count := 0
	for decoder.More() {
		count++
		if count > maxCatalogueEntries {
			return nil, ProviderError{Kind: "oversized_catalogue"}
		}
		var r struct {
			Name string      `json:"name"`
			ID   json.Number `json:"stream_id"`
			EPG  string      `json:"epg_channel_id"`
		}
		if err = decoder.Decode(&r); err != nil {
			return nil, readError(err)
		}
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
	closing, err := decoder.Token()
	if err != nil {
		return nil, readError(err)
	}
	if closing != json.Delim(']') {
		return nil, ProviderError{Kind: "malformed_response"}
	}
	// Reject partial JSON, extra documents, and oversized trailing padding.
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return nil, readError(err)
	}
	if limited.N == 0 {
		return nil, ProviderError{Kind: "oversized_response"}
	}
	return out, nil
}
