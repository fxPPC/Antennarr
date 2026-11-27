package guide

import (
	"bufio"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
)

const maxGuideBytes = 64 << 20
const horizon = 8 * 24 * time.Hour

type station struct {
	id         string
	names      []string
	programmes []Programme
}

func (s *station) displayName() string {
	if len(s.names) > 0 {
		return s.names[0]
	}
	return s.id
}
func xmlTime(s string) (int64, bool) {
	for _, layout := range []string{"20060102150405 -0700", "200601021504 -0700", "20060102150405", "200601021504"} {
		if t, e := time.Parse(layout, strings.TrimSpace(s)); e == nil {
			return t.Unix(), true
		}
	}
	return 0, false
}
func bounded(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return ""
	}
	return s
}

type episode struct {
	System string `xml:"system,attr"`
	Text   string `xml:",chardata"`
}

func first(ss []string, n int) string {
	for _, s := range ss {
		if v := bounded(s, n); v != "" {
			return v
		}
	}
	return ""
}

func parseXMLTV(r io.Reader, now int64) (map[string]*station, error) {
	raw := &io.LimitedReader{R: r, N: maxGuideBytes + 1}
	buf := bufio.NewReader(raw)
	magic, _ := buf.Peek(2)
	var stream io.Reader = buf
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, e := gzip.NewReader(buf)
		if e != nil {
			return nil, e
		}
		defer gz.Close()
		stream = gz
	}
	boundedStream := &io.LimitedReader{R: stream, N: maxGuideBytes + 1}
	d := xml.NewDecoder(boundedStream)
	out := map[string]*station{}
	get := func(id string) *station {
		if id == "" || len(id) > 256 {
			return nil
		}
		s := out[id]
		if s == nil {
			s = &station{id: id}
			out[id] = s
		}
		return s
	}
	depth := 0
	root := false
	count := 0
	for {
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, errors.New("invalid XMLTV")
		}
		switch tag := t.(type) {
		case xml.StartElement:
			if depth == 0 {
				if root || tag.Name.Local != "tv" {
					return nil, errors.New("invalid XMLTV root")
				}
				root = true
				depth++
				continue
			}
			switch tag.Name.Local {
			case "channel":
				var x struct {
					ID    string   `xml:"id,attr"`
					Names []string `xml:"display-name"`
				}
				if e = d.DecodeElement(&x, &tag); e != nil {
					return nil, e
				}
				if st := get(x.ID); st != nil {
					for _, name := range x.Names {
						if v := bounded(name, 256); v != "" && len(st.names) < 8 {
							st.names = append(st.names, v)
						}
					}
				}
			case "programme":
				count++
				var x struct {
					Station      string    `xml:"channel,attr"`
					Start        string    `xml:"start,attr"`
					Stop         string    `xml:"stop,attr"`
					Titles       []string  `xml:"title"`
					Subtitles    []string  `xml:"sub-title"`
					Descriptions []string  `xml:"desc"`
					Episodes     []episode `xml:"episode-num"`
				}
				if e = d.DecodeElement(&x, &tag); e != nil {
					return nil, e
				}
				start, ok := xmlTime(x.Start)
				if !ok || start < now-2*86400 || start > now+int64(horizon.Seconds()) {
					continue
				}
				stop, hasStop := xmlTime(x.Stop)
				if x.Stop != "" && (!hasStop || stop <= start) {
					continue
				}
				p := Programme{Start: start, Stop: stop, Title: first(x.Titles, 1024), Subtitle: first(x.Subtitles, 1024), Description: first(x.Descriptions, 8192)}
				if p.Title == "" {
					continue
				}
				for _, ep := range x.Episodes {
					if n := bounded(ep.Text, 128); n != "" && (p.Episode == "" || ep.System == "xmltv_ns") {
						p.Episode = n
						p.EpisodeSystem = bounded(ep.System, 64)
					}
				}
				if st := get(x.Station); st != nil {
					st.programmes = append(st.programmes, p)
				}
			default:
				if e = d.Skip(); e != nil {
					return nil, e
				}
			}
			if len(out) > 25000 || count > 300000 {
				return nil, errors.New("XMLTV record limit exceeded")
			}
		case xml.EndElement:
			depth--
		}
	}
	if !root || depth != 0 || raw.N == 0 || boundedStream.N == 0 {
		return nil, errors.New("incomplete or oversized XMLTV")
	}
	for _, s := range out {
		sort.SliceStable(s.programmes, func(i, j int) bool { return programmeOrder(s.programmes[i], s.programmes[j]) })
		fixed := s.programmes[:0]
		for i, p := range s.programmes {
			if p.Stop == 0 && i+1 < len(s.programmes) {
				p.Stop = s.programmes[i+1].Start
			}
			if p.Stop <= now || p.Stop <= p.Start || p.Stop-p.Start > 2*86400 {
				continue
			}
			if len(fixed) > 0 && fixed[len(fixed)-1].Stop > p.Start {
				continue
			}
			fixed = append(fixed, p)
		}
		s.programmes = fixed
	}
	return out, nil
}
