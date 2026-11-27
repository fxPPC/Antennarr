package tuner

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"antennarr/internal/guide"
)

type programmeReader interface {
	Programmes(context.Context, string) (map[string][]guide.Programme, error)
}

// Jellyfin also derives its channel identity from the media URL. Bind both the
// M3U guide ID and its playback path to immutable device and channel IDs.
func (s *Server) broadcastID(id string) string {
	key := sha256.Sum256([]byte(strings.ToUpper(s.Config.DeviceID) + "\x00" + id))
	return "antennarr-" + hex.EncodeToString(key[:16])
}

func (s *Server) watchID(w http.ResponseWriter, r *http.Request) {
	for _, ch := range s.channels() {
		if s.broadcastID(ch.ID) == r.PathValue("id") {
			s.serveChannel(w, r, ch)
			return
		}
	}
	http.NotFound(w, r)
}

func playlistText(value string) string {
	return strings.Map(func(r rune) rune {
		// Players can parse quoted key/value pairs anywhere on the EXTINF line.
		if r == '"' {
			return '”'
		}
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, value)
}

// Jellyfin recognises “number. name” as a SAT>IP label and takes the final
// comma-delimited field as the name. Its tvg-name attribute preserves a name
// containing commas if the optional display field is empty.
func playlistLabel(number, name string) string {
	name = playlistText(name)
	if strings.Contains(name, ",") {
		return ` tvg-name="` + name + `",`
	}
	if first, _, ok := strings.Cut(name, " "); ok {
		if _, err := strconv.ParseFloat(strings.Trim(first, "."), 64); err == nil {
			return "," + number + ". " + name
		}
	}
	return "," + name
}

func (s *Server) playlist(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	out := bufio.NewWriter(w)
	fmt.Fprintln(out, "#EXTM3U")
	for _, ch := range s.channels() {
		id := s.broadcastID(ch.ID)
		fmt.Fprintf(out, "#EXTINF:-1 tvg-id=%q tvg-chno=%q%s\n%s/stream/%s\n", id, ch.Number, playlistLabel(ch.Number, ch.Name), strings.TrimRight(s.BaseURL, "/"), id)
	}
	_ = out.Flush()
}

type xmlChannel struct {
	XMLName xml.Name `xml:"channel"`
	ID      string   `xml:"id,attr"`
	Name    string   `xml:"display-name"`
}
type xmlEpisode struct {
	System string `xml:"system,attr,omitempty"`
	Value  string `xml:",chardata"`
}
type xmlProgramme struct {
	XMLName  xml.Name    `xml:"programme"`
	Channel  string      `xml:"channel,attr"`
	Start    string      `xml:"start,attr"`
	Stop     string      `xml:"stop,attr"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"sub-title,omitempty"`
	Desc     string      `xml:"desc,omitempty"`
	Episode  *xmlEpisode `xml:"episode-num,omitempty"`
}

func (s *Server) xmltv(w http.ResponseWriter, r *http.Request) {
	var schedules map[string][]guide.Programme
	if s.Guide != nil {
		profile := s.profileID
		if profile == "" {
			profile = "primary"
		}
		var err error
		schedules, err = s.Guide.Programmes(r.Context(), profile)
		if err != nil {
			http.Error(w, "Programme guide unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	// XML encodes one programme at a time. Buffer across encoder flushes so a
	// week-long guide does not issue one network write per programme.
	buf := bufio.NewWriterSize(w, 32<<10)
	if _, err := io.WriteString(buf, xml.Header); err != nil {
		return
	}
	out := xml.NewEncoder(buf)
	tv := xml.StartElement{Name: xml.Name{Local: "tv"}, Attr: []xml.Attr{{Name: xml.Name{Local: "generator-info-name"}, Value: "Antennarr"}}}
	if err := out.EncodeToken(tv); err != nil {
		return
	}
	channels := s.channels()
	for _, ch := range channels {
		if err := out.Encode(xmlChannel{ID: s.broadcastID(ch.ID), Name: ch.Name}); err != nil {
			return
		}
	}
	for _, ch := range channels {
		id := s.broadcastID(ch.ID)
		for _, p := range schedules[ch.ID] {
			const format = "20060102150405 -0700"
			show := xmlProgramme{
				Channel:  id,
				Start:    time.Unix(p.Start, 0).UTC().Format(format),
				Stop:     time.Unix(p.Stop, 0).UTC().Format(format),
				Title:    p.Title,
				Subtitle: p.Subtitle,
				Desc:     p.Description,
			}
			if p.Episode != "" {
				show.Episode = &xmlEpisode{System: p.EpisodeSystem, Value: p.Episode}
			}
			if err := out.Encode(show); err != nil {
				return
			}
		}
	}
	if err := out.EncodeToken(tv.End()); err != nil {
		return
	}
	if err := out.Flush(); err == nil {
		_ = buf.Flush()
	}
}
