package catalog

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed data/countries.json
var countryData []byte

var countryAliases, countryAliasMax = loadCountryAliases()

func loadCountryAliases() (map[string]string, int) {
	var records map[string][]string
	if e := json.Unmarshal(countryData, &records); e != nil {
		panic("invalid embedded country metadata")
	}
	aliases := map[string]string{}
	longest := 1
	add := func(name, code string) {
		key := strings.Join(nameWords(name), " ")
		if key != "" {
			aliases[key] = code
			longest = max(longest, len(strings.Fields(key)))
		}
	}
	for code, names := range records {
		// Preserve the existing internal UK spelling while accepting ISO GB/GBR.
		if code == "gb" {
			code = "uk"
		}
		for _, name := range names {
			add(name, code)
		}
	}
	for name, code := range map[string]string{"UK": "uk", "U.S.A.": "us", "United States of America": "us", "South Korea": "kr", "North Korea": "kp", "UAE": "ae", "Russia": "ru", "Vietnam": "vn", "Turkey": "tr", "Cote d Ivoire": "ci"} {
		add(name, code)
	}
	return aliases, longest
}

// Country tags belong at label boundaries. Never strip ordinary short words
// from inside channel names ("Made in Chelsea", "It Takes Two", etc.).
// Ambiguous short codes require explicit brackets in a provider name, but not in the
// explicit Country field. Full names and other codes are case-insensitive.
func splitCountry(ts, original []string, explicit bool) ([]string, []string, string) {
	// CW is also a network name; only [CW] or an explicit region means Curaçao.
	ambiguous := " am as at be by cw do in is it me my no so to tv ms id fi geo btn "
	country := ""
	for pass := 0; pass < 2; pass++ {
		found := false
		for n := min(countryAliasMax, len(ts)); n > 0 && !found; n-- {
			if !explicit && n == len(ts) {
				continue
			} // A country-only channel name remains a name.
			for _, start := range []int{0, len(ts) - n} {
				key := strings.Join(ts[start:start+n], " ")
				code := countryAliases[key]
				if code == "" || (!explicit && key == "tv" && !strings.HasPrefix(original[start], "[")) {
					continue
				}
				if !explicit && n == 1 && strings.Contains(ambiguous, " "+key+" ") && !strings.HasPrefix(original[start], "[") {
					continue
				}
				if country != "" && country != code {
					return ts, original, "conflict"
				}
				country = code
				ts = append(append([]string{}, ts[:start]...), ts[start+n:]...)
				original = append(append([]string{}, original[:start]...), original[start+n:]...)
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	// Recognise clearly tagged or unambiguous country codes between other
	// name words too, e.g. "Channel NZ North". Ordinary words remain literal.
	for i := 1; i+1 < len(ts); {
		key := ts[i]
		code := countryAliases[key]
		tagged := strings.HasPrefix(original[i], "[")
		ambiguousWord := strings.Contains(ambiguous, " "+key+" ") || strings.Contains(" can are and ago arm cod fin guy gum jam lie mac mar nor pan per pry sur ton vat ms ", " "+key+" ")
		if code != "" && (tagged || (!ambiguousWord && len(key) <= 3)) {
			if country != "" && country != code {
				return ts, original, "conflict"
			}
			country = code
			ts = append(ts[:i], ts[i+1:]...)
			original = append(original[:i], original[i+1:]...)
		} else {
			i++
		}
	}
	return ts, original, country
}
func regionCountry(region string) string {
	raw := originalWords.FindAllString(normaliseName(region), -1)
	ts := make([]string, len(raw))
	for i, t := range raw {
		ts[i] = strings.ToLower(t)
	}
	_, _, country := splitCountry(ts, raw, true)
	return country
}
