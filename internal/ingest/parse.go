// Package ingest parses user-authorised material only. It never makes requests.
package ingest

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
)

type Entry struct{ Kind, Endpoint, Username, Password, URL, Name, TVGID, Region string }

var attr = regexp.MustCompile(`([\w-]+)="([^"]*)"`)

func Canonical(raw string) (string, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u.Host == "" || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") || u.Opaque != "" {
		return "", errors.New("unsupported URL")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if strings.ContainsAny(host, "\r\n\x00") || host == "" {
		return "", errors.New("invalid host")
	}
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	u.Fragment = ""
	q := u.Query()
	u.RawQuery = q.Encode()
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}
func FromURL(raw, name, tvg string) (Entry, error) {
	normalized, e := Canonical(raw)
	if e != nil {
		return Entry{}, e
	}
	u, _ := url.Parse(normalized)
	out := Entry{Kind: "direct", URL: normalized, Name: strings.TrimSpace(name), TVGID: tvg}
	if out.Name == "" {
		out.Name = "Unnamed stream"
	}
	q := u.Query()
	if (strings.HasSuffix(u.Path, "/get.php") || strings.HasSuffix(u.Path, "/player_api.php")) && q.Get("username") != "" && q.Get("password") != "" {
		out.Kind = "xtream"
		out.Username = q.Get("username")
		out.Password = q.Get("password")
		u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "get.php"), "player_api.php")
		u.RawQuery = ""
		u.User = nil
		out.Endpoint = strings.TrimRight(u.String(), "/")
		out.URL = ""
		return out, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 4 && parts[len(parts)-4] == "live" {
		out.Kind = "xtream"
		out.Username = parts[len(parts)-3]
		out.Password = parts[len(parts)-2]
		u.Path = "/" + strings.Join(parts[:len(parts)-4], "/")
		u.RawPath = ""
		u.RawQuery = ""
		u.User = nil
		out.Endpoint = strings.TrimRight(u.String(), "/")
	} else {
		out.Endpoint = u.Scheme + "://" + u.Host
	}
	return out, nil
}

var errNoRecords = errors.New("no complete source records found")

func Parse(data []byte) ([]Entry, error) {
	if len(data) > 2<<20 {
		return nil, errors.New("import exceeds 2 MiB")
	}
	if strings.HasPrefix(strings.TrimSpace(string(data)), "[") || strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		if rows, e := flexible(string(data)); e == nil {
			return rows, nil
		}
	}
	rows, err := parse(data, 0)
	if err == nil {
		return rows, nil
	}
	rows, err = flexible(string(data))
	if err != nil || len(rows) > 20000 {
		return nil, errNoRecords
	}
	return rows, nil
}
func parse(data []byte, depth int) ([]Entry, error) {
	if len(data) > 2<<20 {
		return nil, errors.New("import exceeds 2 MiB")
	}
	s := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if s == "" {
		return nil, errors.New("empty import")
	}
	if depth < 2 && !strings.ContainsAny(s, "\n :") {
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if b, e := enc.DecodeString(s); e == nil && (strings.Contains(string(b), "http") || strings.Contains(string(b), "#EXTM3U")) {
				return parse(b, depth+1)
			}
		}
	}
	if depth < 2 && (strings.HasPrefix(strings.ToLower(s), "http%3a") || strings.HasPrefix(strings.ToLower(s), "https%3a")) {
		if decoded, e := url.QueryUnescape(s); e == nil {
			return parse([]byte(decoded), depth+1)
		}
	}
	if strings.Contains(s, "#EXT-X-TARGETDURATION") || strings.Contains(s, "#EXT-X-STREAM-INF") {
		return nil, errors.New("import an HLS playlist URL rather than its segment listing")
	}
	if strings.HasPrefix(s, "[") {
		var rows []struct {
			URL      string `json:"url"`
			Name     string `json:"name"`
			Server   string `json:"server"`
			Username string `json:"username"`
			Password string `json:"password"`
			TVGID    string `json:"tvg_id"`
		}
		if json.Unmarshal([]byte(s), &rows) != nil {
			return nil, errors.New("invalid JSON list")
		}
		out := []Entry{}
		for _, r := range rows {
			raw := r.URL
			if raw == "" {
				normalized, e := Canonical(r.Server)
				if e != nil || r.Username == "" || r.Password == "" {
					return nil, errors.New("invalid account record")
				}
				u, _ := url.Parse(normalized)
				if u.User != nil || u.RawQuery != "" {
					return nil, errors.New("invalid account endpoint")
				}
				out = append(out, Entry{Kind: "xtream", Endpoint: strings.TrimRight(normalized, "/"), Username: r.Username, Password: r.Password, Name: r.Name})
				continue
			}
			entry, e := FromURL(raw, r.Name, r.TVGID)
			if e != nil {
				return nil, e
			}
			out = append(out, entry)
		}
		if len(out) == 0 || len(out) > 20000 {
			return nil, errors.New("invalid import size")
		}
		return out, nil
	}
	scan := bufio.NewScanner(strings.NewReader(s))
	scan.Buffer(make([]byte, 4096), 1<<20)
	out := []Entry{}
	name, tvg := "", ""
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if strings.HasPrefix(line, "#EXTINF:") {
			if i := strings.LastIndex(line, ","); i >= 0 {
				name = strings.TrimSpace(line[i+1:])
			}
			for _, m := range attr.FindAllStringSubmatch(line, -1) {
				if m[1] == "tvg-id" {
					tvg = m[2]
				}
				if m[1] == "tvg-name" && name == "" {
					name = m[2]
				}
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Plain-text records use server|username|password; never interpret shell text.
		if parts := strings.Split(line, "|"); len(parts) == 3 {
			server, e := Canonical(parts[0])
			if e != nil || parts[1] == "" || parts[2] == "" {
				return nil, errors.New("invalid credential record")
			}
			out = append(out, Entry{Kind: "xtream", Endpoint: strings.TrimRight(server, "/"), Username: parts[1], Password: parts[2], Name: name})
		} else {
			entry, e := FromURL(line, name, tvg)
			if e != nil {
				return nil, e
			}
			out = append(out, entry)
		}
		name, tvg = "", ""
		if len(out) > 20000 {
			return nil, errors.New("too many import records")
		}
	}
	if scan.Err() != nil {
		return nil, errors.New("invalid or oversized import line")
	}
	if len(out) == 0 {
		return nil, errors.New("no source records")
	}
	return out, nil
}
