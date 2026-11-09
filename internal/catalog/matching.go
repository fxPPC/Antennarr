package catalog

import (
	"antennarr/internal/ingest"
	"html"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var words = regexp.MustCompile(`[\p{L}\p{M}\p{N}]+`)
var originalWords = words

var qualityPlus = regexp.MustCompile(`(?i)\b(fhd|uhd|hd|sd)\+($|\s)`)

func normaliseName(s string) string {
	s = qualityPlus.ReplaceAllString(norm.NFKC.String(html.UnescapeString(s)), "$1$2")
	return strings.NewReplacer("+", " plus ", "&", " and ").Replace(s)
}
func nameWords(s string) []string { return words.FindAllString(strings.ToLower(normaliseName(s)), -1) }

func exclusionWords(s string) []string {
	out := []string{}
	for _, t := range nameWords(s) {
		if m := compactQuality.FindStringSubmatch(t); len(m) == 3 && !quality.MatchString(t) {
			out = append(out, m[1], m[2])
		} else {
			out = append(out, t)
		}
	}
	return out
}

// Mark clearly delimited country tags without changing token positions.
func sourceWords(name string) []string {
	locs := words.FindAllStringIndex(name, -1)
	out := make([]string, len(locs))
	for i, l := range locs {
		out[i] = name[l[0]:l[1]]
		after := strings.TrimSpace(name[l[1]:])
		// A separator before a brand is not evidence that the brand is a country.
		// For example, US | TV Land must not mark TV as Tuvalu.
		if after != "" && strings.ContainsAny(after[:1], "])}") {
			out[i] = "[" + out[i] + "]"
		}
	}
	return out
}

var listingNumber = regexp.MustCompile(`^\s*[0-9]{1,5}\s*[:.|-]\s*`)
var compactQuality = regexp.MustCompile(`(?i)([\p{L}\p{N}][\p{L}\p{M}\p{N}]*?)(fhd|uhd|hd|sd|1080p|720p|2160p)$`)

// UHD wins over an attached HD suffix: ESPNUHD means ESPN UHD.
// HBOFHD remains ambiguous with HBO Family's HBOF abbreviation plus HD.
func compactQualityToken(t string) string {
	if quality.MatchString(t) || t == "hbofhd" {
		return t
	}
	return compactQuality.ReplaceAllString(t, "$1")
}

var quality = regexp.MustCompile(`^(?:sd|hd|fhd|uhd|4k|8k|hevc|h264|h265|raw|backup|1080[pi]?|720p?|480p?|2160p?|[0-9]+fps|(?:1080|720|2160)[pi][0-9]+(?:fps)?|hdr|hfr|avc|hvec|50hz|60hz)$`)

// Match names as token sequences, not substrings: MTV 2 and CNN International
// must not match MTV and CNN. Regional words stay in the name; aliases and exclusions decide matches.
func channelKey(name, region string) (string, string) {
	name = listingNumber.ReplaceAllString(normaliseName(name), "")
	// Track bracketed country tags without turning ordinary brand words into tags.
	name = strings.NewReplacer("h.264", "h264", "H.264", "h264").Replace(name)
	name = strings.NewReplacer("h.265", "h265", "H.265", "h265").Replace(name)
	original := sourceWords(name)
	ts := words.FindAllString(strings.ToLower(name), -1)
	for i, t := range ts {
		ts[i] = compactQualityToken(t)
	}
	if len(ts) > 1 && ts[0] == "live" {
		ts = ts[1:]
		original = original[1:]
	}
	// Remove transport quality tags before recognising boundary country labels.
	filtered, rawFiltered := []string{}, []string{}
	for i, t := range ts {
		if !quality.MatchString(t) {
			filtered = append(filtered, t)
			rawFiltered = append(rawFiltered, original[i])
		}
	}
	ts, original, initialCountry := splitCountry(filtered, rawFiltered, false)
	out := []string{}
	rawOut := []string{}
	country := initialCountry
	for i, t := range ts {
		out = append(out, t)
		rawOut = append(rawOut, original[i])
	}
	out, _, detected := splitCountry(out, rawOut, false)
	if country == "" {
		country = detected
	}
	if country == "" {
		country = regionCountry(region)
	}
	key := strings.Join(out, " ")
	// Canonical brand aliases preserve other words (regional variants,
	// spin-offs, and timeshifts), instead of swallowing those distinctions.
	for _, pair := range [][2]string{{"fox news channel", "fox news"}, {"foxnewschannel", "fox news"}, {"foxnews", "fox news"}, {"discovery channel", "discovery"}, {"discoverychannel", "discovery"}, {"the learning channel", "tlc"}, {"animalplanet", "animal planet"}, {"ms now", "msnow"}, {"msnbc", "msnow"}, {"mtv 1", "mtv"}, {"mtv1", "mtv"}, {"vh 1", "vh1"}} {
		key = strings.TrimSpace(strings.ReplaceAll(" "+key+" ", " "+pair[0]+" ", " "+pair[1]+" "))
	}
	return key, country
}

// Compile each channel's aliases once per catalogue pass. Provider labels are
// parsed once per entry, keeping bulk imports off the hot playback path.
type channelLabel struct{ key, country string }
type channelMatcher struct {
	channel  Channel
	positive []channelLabel
	negative []string
}
type sourceLabel struct {
	channelLabel
	words     string
	ambiguous bool
}

func parseSourceLabel(name, region string) sourceLabel {
	k, c := channelKey(name, region)
	label := sourceLabel{channelLabel: channelLabel{k, c}, words: " " + strings.Join(exclusionWords(name), " ") + " "}
	for _, token := range strings.Fields(k) {
		if token == "hbofhd" {
			label.ambiguous = true
		}
	}
	return label
}
func sourceLabelWithID(name, region, id string) sourceLabel {
	label := parseSourceLabel(name, region)
	// Separate fields with two spaces so a phrase cannot straddle name and ID.
	if id != "" {
		label.words += " " + strings.Join(exclusionWords(id), " ") + " "
	}
	return label
}
func compileChannel(c Channel) channelMatcher {
	m := channelMatcher{channel: c}
	for _, alias := range append([]string{c.Name}, c.Aliases...) {
		alias = strings.TrimSpace(alias)
		if strings.HasPrefix(alias, "-") {
			phrase := strings.Join(exclusionWords(strings.TrimSpace(strings.TrimPrefix(alias, "-"))), " ")
			if phrase != "" {
				m.negative = append(m.negative, " "+phrase+" ")
			}
			continue
		}
		k, country := channelKey(alias, c.Region)
		m.positive = append(m.positive, channelLabel{k, country})
	}
	return m
}
func (m channelMatcher) excludes(label sourceLabel) bool {
	for _, phrase := range m.negative {
		if strings.Contains(label.words, phrase) {
			return true
		}
	}
	return false
}

// Learned provider IDs may supply alternate names, but must not silently
// cross an explicit country boundary. Manual assignments remain explicit.
func (m channelMatcher) compatible(label sourceLabel) bool {
	if label.ambiguous || label.country == "conflict" {
		return false
	}
	for _, p := range m.positive {
		if p.key != "" && (p.country == "" || label.country == "" || p.country == label.country) {
			return true
		}
	}
	return false
}
func (m channelMatcher) matches(label sourceLabel) bool {
	if m.excludes(label) || !m.compatible(label) {
		return false
	}
	for _, p := range m.positive {
		if p.key != "" && label.key != "" && p.key == label.key && (p.country == "" || label.country == "" || p.country == label.country) {
			return true
		}
	}
	return false
}

// RelevantEntries limits automatic provider refreshes to selected channels.
// Manual imports can still catalogue explicitly supplied playlists in full.
func (s *Store) RelevantEntries(entries []ingest.Entry) ([]ingest.Entry, error) {
	channels, e := s.Channels()
	if e != nil {
		return nil, e
	}
	rows, e := s.DB.Query("SELECT channel_id,alias FROM channel_aliases")
	if e != nil {
		return nil, e
	}
	known := map[string]map[string]bool{}
	for rows.Next() {
		var a, ch string
		if e = rows.Scan(&ch, &a); e != nil {
			rows.Close()
			return nil, e
		}
		if known[a] == nil {
			known[a] = map[string]bool{}
		}
		known[a][ch] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	// Fetch a bounded reserve only where output still needs alternatives.
	state, e := s.RosterState()
	if e != nil {
		return nil, e
	}
	room := map[string]int{}
	uhdRoom := map[string]int{}
	for _, ch := range state.Channels {
		if ch.Count >= ch.Policy.Target {
			continue
		}
		var saved, savedUHD int
		if e = s.DB.QueryRow(`SELECT coalesce(sum(instr(upper(s.name),'UHD')=0),0),coalesce(sum(instr(upper(s.name),'UHD')>0),0) FROM mappings m JOIN streams s ON s.id=m.stream_id WHERE m.channel_id=? AND m.enabled=1 AND s.enabled=1 AND s.state NOT IN ('retired','manually_blocked')`, ch.ChannelID).Scan(&saved, &savedUHD); e != nil {
			return nil, e
		}
		room[ch.ChannelID] = max(0, max(60, 3*ch.Policy.Target)-saved)
		uhdRoom[ch.ChannelID] = max(0, max(60, 3*ch.Policy.Target)-savedUHD)
	}
	matchers := make([]channelMatcher, 0, len(channels))
	for _, c := range channels {
		matchers = append(matchers, compileChannel(c))
	}
	out := []ingest.Entry{}
	for _, entry := range entries {
		if !s.UHDAllowed() && IsUHD(entry.Name) {
			continue
		}
		match := false
		label := sourceLabelWithID(entry.Name, entry.Region, entry.TVGID)
		candidateRoom := room
		if IsUHD(entry.Name) {
			candidateRoom = uhdRoom
		}
		for _, c := range matchers {
			if candidateRoom[c.channel.ID] > 0 && c.channel.Enabled && !c.excludes(label) && (c.matches(label) || (entry.TVGID != "" && c.compatible(label) && known["tvg:"+strings.ToLower(entry.TVGID)][c.channel.ID])) {
				candidateRoom[c.channel.ID]--
				match = true
				break
			}
		}

		if match {
			out = append(out, entry)
		}
	}
	return out, nil
}
