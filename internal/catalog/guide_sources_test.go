package catalog

import "testing"

func TestGuideRegionalAndTimeShiftIdentity(t *testing.T) {
	c := Channel{ID: "example", Name: "Legend Xtra", Region: "GB"}
	for _, id := range []string{"LegendXtraPlus1.uk", "LegendXtra.us"} {
		if GuideAllowed(c, id, "Legend Xtra") {
			t.Fatal("allowed", id)
		}
	}
	if GuideAllowed(c, "opaque", "Legend Xtra Plus1") {
		t.Fatal("display-name delay accepted")
	}
	if !GuideAllowed(c, "LegendXtra.uk", "Legend Xtra") {
		t.Fatal("base service rejected")
	}
	c.Name = "Legend Xtra +1"
	if !GuideAllowed(c, "LegendXtraPlus1.uk", "Legend Xtra +1") {
		t.Fatal("correct delayed service rejected")
	}
}
