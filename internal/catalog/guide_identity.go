package catalog

import (
	"regexp"
	"strings"
)

var guideTimeshift = regexp.MustCompile(`^(.+?)plus([1-9][0-9]?)$`)

func timeshiftGuide(guide channelLabel) (channelLabel, bool) {
	parts := guideTimeshift.FindStringSubmatch(strings.ReplaceAll(guide.key, " ", ""))
	if parts == nil {
		return channelLabel{}, false
	}
	return channelLabel{key: parts[1], country: guide.country}, true
}

// A dotted guide-ID country suffix is explicit metadata, unlike an ordinary
// word in a display name: CartoonNetwork.in is Indian, but STARZ in Black is
// not. Do not interpret arbitrary domains or numeric provider IDs as names.
func guideKey(id string) (string, string) {
	id = strings.TrimSpace(id)
	// Use the final component; IDs can contain several dotted brand words.
	if dot := strings.LastIndex(id, "."); dot >= 0 {
		base, suffix := id[:dot], id[dot+1:]
		if len(suffix) < 2 || len(suffix) > 3 {
			return channelKey(id, "")
		}
		if country := countryAliases[strings.ToLower(suffix)]; country != "" {
			key, detected := channelKey(base, "")
			if detected != "" && detected != country {
				return key, "conflict"
			}
			return key, country
		}
	}
	return channelKey(id, "")
}
