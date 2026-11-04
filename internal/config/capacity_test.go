package config

import "testing"

func TestViewerCapacityAdjustment(t *testing.T) {
	var old Settings
	old.Defaults()
	for _, n := range []int{1, 3, 4, 6, 64} {
		v := old
		v.MaxOutgoingStreams = n
		v = AdjustViewerCapacity(v, old)
		if e := v.Validate(); e != nil {
			t.Fatal(n, e)
		}
		if n > 3 && v.GlobalConnections < 2*n+v.StartupRace-2 {
			t.Fatal("standby/startup capacity missing", v)
		}
		if n < 3 && v.GlobalConnections != old.GlobalConnections {
			t.Fatal("lowering viewers shrank upstream budget")
		}
	}
	v := old
	v.MaxOutgoingStreams = 6
	v.GlobalConnections = 20
	if AdjustViewerCapacity(v, old).GlobalConnections != 20 {
		t.Fatal("explicit cap overwritten")
	}
}
