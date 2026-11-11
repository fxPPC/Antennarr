package catalog

import (
	"reflect"
	"strings"
	"testing"
)

// Exercise the shipped preset, not a copy of its rules: every broadcast alias
// must select only its intended service, including country and quality labels.
func TestBroadcastPresetMatching(t *testing.T) {
	channels, err := DefaultUSChannels()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"CBS": "002", "NBC": "004", "FOX": "005", "ABC": "007", "MyNetworkTV": "009", "The CW": "011", "PBS": "013"}
	matchers := make([]channelMatcher, len(channels))
	for i, c := range channels {
		matchers[i] = compileChannel(c)
	}
	for i, c := range channels {
		number, broadcast := want[c.Name]
		if !broadcast {
			continue
		}
		if c.Number != number || c.Region != "US" || !c.Enabled || len(c.Aliases) > 200 {
			t.Fatalf("invalid broadcast preset: %+v", c)
		}
		delete(want, c.Name)
		for _, alias := range append([]string{c.Name}, c.Aliases...) {
			if strings.HasPrefix(alias, "-") {
				if !matchers[i].excludes(parseSourceLabel(c.Name+" "+strings.TrimPrefix(alias, "-"), "US")) {
					t.Errorf("%s exclusion %q did not block", c.Name, alias)
				}
				continue
			}
			for _, decorated := range []string{strings.ToLower(alias), "US | " + alias + " HD", "[USA] " + alias + " FHD", alias + " | US", "001 | US | " + alias + " 1080p"} {
				label := parseSourceLabel(decorated, "")
				for j, m := range matchers {
					if got := m.matches(label); got != (i == j) {
						t.Errorf("%s alias %q matched %s: %v", c.Name, decorated, m.channel.Name, got)
					}
				}
			}
			for _, rejected := range []string{"UK | " + alias, "CA | " + alias, "AU | " + alias, alias + " West", alias + " Pacific", alias + " On Demand"} {
				if matchers[i].matches(parseSourceLabel(rejected, "")) {
					t.Errorf("%s accepts %q", c.Name, rejected)
				}
			}
		}
	}
	if len(want) != 0 {
		t.Fatal("missing broadcast channels", want)
	}
}

func TestBroadcastPresetRejectsOtherServices(t *testing.T) {
	channels, err := DefaultUSChannels()
	if err != nil {
		t.Fatal(err)
	}
	rejected := map[string][]string{
		"CBS":         {"CBS Sports Network", "CBS News 24/7", "CBS Reality", "WCBS Start TV", "CBS Boston WBZ"},
		"NBC":         {"NBC News Now", "NBC Sports Philadelphia", "NBC Universo", "WNBC Cozi TV", "NBC Los Angeles KNBC"},
		"FOX":         {"FOX News", "FOX Business", "FOX Sports 1", "FOX Weather", "FOX Soul", "WNYW Movies!", "FOX Los Angeles KTTV"},
		"ABC":         {"ABC News Live", "ABC Family", "ABC Kids", "WABC Localish", "ABC Los Angeles KABC"},
		"MyNetworkTV": {"WWOR Buzzr", "MyNetworkTV Los Angeles KCOP"},
		"The CW":      {"WPIX Antenna TV", "WPIX Rewind TV", "The CW West", "The CW Los Angeles KTLA"},
		"PBS":         {"PBS Kids", "PBS Create", "PBS World", "PBS America", "WNET PBS Kids", "PBS Boston WGBH"},
	}
	for _, c := range channels {
		for _, label := range rejected[c.Name] {
			if nameMatches(c, "US | "+label+" HD", "") {
				t.Errorf("%s accepts different service %q", c.Name, label)
			}
		}
	}
}

func TestExistingBroadcastLineupPreservedOnStartup(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	c := Channel{ID: "custom-nbc", Number: "404", Name: "NBC", Region: "US", Enabled: false, Aliases: []string{"WNBC", "-west", "-news"}}
	if _, err = s.SaveChannel(c); err != nil {
		s.Close()
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenWithUSDefaults(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Channels()
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], c) {
		t.Fatalf("saved channel changed or preset expanded on startup: %+v, %v", got, err)
	}
}
