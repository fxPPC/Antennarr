// Package ingest's portal adapter reads operator-provisioned subscriber exports.
// The REST variant reads one registered subscription and intersects its channel
// IDs with the operator's export. No device identity is presented as a login.
package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"antennarr/internal/netguard"
)

type PortalAccount struct {
	PortalURL     string `json:"portal_url"`
	MAC           string `json:"mac"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	GuideURL      string `json:"guide_url,omitempty"`
	PlaylistGuide string `json:"playlist_guide,omitempty"`
	PlaylistURL   string `json:"playlist_url,omitempty"`
}

// XMLTV returns the operator override or the guide advertised by the playlist.
func (a PortalAccount) XMLTV() string {
	if a.GuideURL != "" {
		return a.GuideURL
	}
	return a.PlaylistGuide
}

func validGuide(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 8192 && u.Scheme == "https" && u.User == nil && netguard.URL(u) == nil
}

type PortalChannel struct {
	Key, Name, TVGID, URL string
}

func (a *PortalAccount) Normalise() error {
	mac, err := net.ParseMAC(strings.TrimSpace(a.MAC))
	if err != nil || len(mac) != 6 || mac[0]&1 != 0 || bytes.Equal(mac, make([]byte, 6)) {
		return errors.New("enter the registered six-byte unicast MAC address")
	}
	a.MAC = strings.ToUpper(mac.String())
	a.PortalURL = strings.TrimSpace(a.PortalURL)
	a.PlaylistURL = strings.TrimSpace(a.PlaylistURL)
	a.GuideURL = strings.TrimSpace(a.GuideURL)
	if a.GuideURL != "" && !validGuide(a.GuideURL) {
		return errors.New("enter a public HTTPS XMLTV URL")
	}
	u, err := url.Parse(a.PortalURL)
	if err != nil || u.Scheme != "https" || len(a.PortalURL) > 2048 || u.User != nil || u.RawQuery != "" || netguard.URL(u) != nil {
		return errors.New("enter the public HTTPS portal base URL")
	}
	raw, _ := Canonical(a.PortalURL)
	// Operators also give subscribers the standard middleware web client URL.
	a.PortalURL = strings.TrimSuffix(strings.TrimRight(raw, "/"), "/c")
	if (a.Username == "") != (a.Password == "") || strings.Contains(a.Username, ":") || len(a.Username) > 256 || len(a.Password) > 2048 {
		return errors.New("enter both subscriber credentials or leave both blank")
	}
	if a.PlaylistURL == "" && a.Username == "" {
		return errors.New("supply an account playlist URL or subscriber-scoped REST credentials")
	}
	if a.PlaylistURL != "" {
		p, err := url.Parse(a.PlaylistURL)
		if err != nil || len(a.PlaylistURL) > 8192 || p.Scheme != "https" || p.User != nil || netguard.URL(p) != nil {
			return errors.New("enter the operator-issued public HTTPS account playlist URL")
		}
		if strings.HasSuffix(strings.TrimRight(p.Path, "/"), "/server/tools/m3u.php") {
			return errors.New("use subscriber REST credentials for the portal-wide export")
		}
		if a.Username != "" && !strings.EqualFold(u.Host, p.Host) {
			return errors.New("subscriber credentials may only be sent to the configured portal host")
		}
	}
	return nil
}

func (a PortalAccount) read(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || netguard.URL(u) != nil {
		return nil, errors.New("invalid portal destination")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("invalid portal request")
	}
	if a.Username != "" {
		req.SetBasicAuth(a.Username, a.Password)
	}
	req.Header.Set("User-Agent", "Antennarr (subscriber playlist client)")
	// Credentials and signed URLs are scoped to the explicitly configured origin.
	httpClient := *client
	httpClient.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 || r.URL.Scheme != "https" || !strings.EqualFold(r.URL.Host, u.Host) || r.URL.User != nil || netguard.URL(r.URL) != nil {
			return errors.New("portal redirected outside its configured origin")
		}
		return nil
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("portal request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, errors.New("portal denied access to this subscription")
	}
	if resp.StatusCode != 200 {
		return nil, errors.New("portal did not return the requested subscriber data")
	}
	const limit = 2 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(b) > limit {
		return nil, errors.New("portal response incomplete or larger than 2 MiB")
	}
	return b, nil
}

type portalDevice struct {
	MAC      string        `json:"mac"`
	Status   json.Number   `json:"status"`
	Channels []json.Number `json:"sub_ch"`
}
type portalStation struct {
	ID     json.Number `json:"id"`
	Name   string      `json:"name"`
	Number json.Number `json:"number"`
}

func portalRows[T any](ctx context.Context, client *http.Client, a PortalAccount, resource string) ([]T, error) {
	b, err := a.read(ctx, client, a.PortalURL+"/api/"+resource)
	if err != nil {
		return nil, err
	}
	var result struct {
		Status  string
		Results []T
	}
	if json.Unmarshal(b, &result) != nil || result.Status != "OK" || result.Results == nil || len(result.Results) > 20000 {
		return nil, errors.New("invalid subscriber API response")
	}
	return result.Results, nil
}

func (a PortalAccount) subscribedStations(ctx context.Context, client *http.Client) (map[string]string, error) {
	stb, err := portalRows[portalDevice](ctx, client, a, "stb/"+a.MAC)
	if err != nil {
		return nil, err
	}
	if len(stb) != 1 || !strings.EqualFold(stb[0].MAC, a.MAC) || stb[0].Status.String() != "1" {
		return nil, errors.New("registered device is unavailable or inactive")
	}
	sub, err := portalRows[portalDevice](ctx, client, a, "itv_subscription/"+a.MAC)
	if err != nil {
		return nil, err
	}
	if len(sub) != 1 || !strings.EqualFold(sub[0].MAC, a.MAC) || sub[0].Channels == nil {
		return nil, errors.New("subscription does not identify the registered device")
	}
	entitled := map[string]bool{}
	for _, id := range sub[0].Channels {
		entitled[id.String()] = true
	}
	stations, err := portalRows[portalStation](ctx, client, a, "itv")
	if err != nil {
		return nil, err
	}
	// Include ambiguous labels in this check even when only one ID is entitled.
	labels := map[string]string{}
	for _, s := range stations {
		display := s.Number.String() + ". " + strings.TrimSpace(s.Name)
		if _, found := labels[display]; found || !entitled[s.ID.String()] || s.ID == "" || s.Name == "" {
			labels[display] = ""
		} else {
			labels[display] = s.ID.String()
		}
	}
	return labels, nil
}

// A display name starts at the first comma outside an attribute value.
func portalName(line string) string {
	quoted := false
	for i, c := range line {
		switch c {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				return strings.TrimSpace(line[i+1:])
			}
		}
	}
	return ""
}

func (a *PortalAccount) Channels(ctx context.Context, client *http.Client) ([]PortalChannel, error) {
	if err := a.Normalise(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("portal client unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var entitled map[string]string
	export := a.PlaylistURL
	if export == "" {
		var err error
		entitled, err = a.subscribedStations(ctx, client)
		if err != nil {
			return nil, err
		}
		export = a.PortalURL + "/server/tools/m3u.php"
	}
	b, err := a.read(ctx, client, export)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(strings.TrimPrefix(string(b), "\ufeff")), "\n")
	if fields := strings.Fields(lines[0]); len(fields) == 0 || fields[0] != "#EXTM3U" {
		return nil, errors.New("portal export is not an extended M3U playlist")
	}
	a.PlaylistGuide = ""
	for _, m := range attr.FindAllStringSubmatch(lines[0], -1) {
		if m[1] == "x-tvg-url" || m[1] == "url-tvg" {
			for _, candidate := range strings.Split(m[2], ",") {
				if candidate = strings.TrimSpace(candidate); validGuide(candidate) {
					a.PlaylistGuide = candidate
					break
				}
			}
			if a.PlaylistGuide != "" {
				break
			}
		}
	}
	var out []PortalChannel
	seen := map[string]bool{}
	ambiguous := map[string]bool{}
	name, tvg := "", ""
	described, usable := false, false
	for _, s := range lines[1:] {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "#EXT-X-") {
			return nil, errors.New("use a channel catalogue rather than an HLS stream")
		}
		if strings.HasPrefix(s, "#EXTINF:") {
			described = true
			name, tvg = portalName(s), ""
			for _, m := range attr.FindAllStringSubmatch(s, -1) {
				if m[1] == "tvg-id" {
					tvg = m[2]
				}
			}
			continue
		}
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		described = true
		station, id := name, tvg
		name, tvg = "", ""
		if station == "" || len(station) > 512 || len(id) > 256 {
			continue
		}
		u, err := url.Parse(s)
		if err != nil || len(s) > 8192 || u.User != nil || netguard.URL(u) != nil {
			continue
		}
		usable = true
		key := "playlist\x00" + station + "\x00" + id
		if entitled != nil {
			channelID := entitled[station]
			if channelID == "" {
				continue
			}
			key = "station\x00" + channelID
			_, station, _ = strings.Cut(station, ". ")
		}
		h := sha256.Sum256([]byte(key))
		key = hex.EncodeToString(h[:])
		if seen[key] {
			ambiguous[key] = true
			continue
		}
		seen[key] = true
		out = append(out, PortalChannel{key, station, id, u.String()})
		if len(out) > 20000 {
			return nil, errors.New("portal channel limit exceeded")
		}
	}
	if described && !usable {
		return nil, errors.New("no usable HTTP channels in portal response")
	}
	filtered := out[:0]
	for _, c := range out {
		if !ambiguous[c.Key] {
			filtered = append(filtered, c)
		}
	}
	return filtered, nil
}
