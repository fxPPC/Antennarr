package media

import "testing"

func TestAudioFramingRejectsCorruptionDespiteAdvancingPTS(t *testing.T) {
	var a Analyzer
	a.SetAudioCodec("aac")
	packet := func(stamp byte, h1, h2 byte) []byte {
		b := make([]byte, 188)
		b[0] = 0x47
		b[1] = 0x41
		b[2] = 1
		b[3] = 0x10
		b[6] = 1
		b[7] = 0xc0
		b[10] = 0x80
		b[11] = 0x80
		b[12] = 5
		b[17] = stamp
		b[18] = h1
		b[19] = h2
		return b
	}
	for i := byte(1); i <= 3; i++ {
		if e := a.Push(packet(i*2, 0, 0)); e != nil {
			t.Fatal(e)
		}
	}
	s := a.Snapshot()
	if s.AudioChanges != 3 || s.LastAudio.IsZero() || s.InvalidAudio != 3 {
		t.Fatalf("bad audio mistaken for healthy media: %+v", s)
	}
	a.Push(packet(8, 0xff, 0xf1))
	if a.Snapshot().InvalidAudio != 0 {
		t.Fatal("valid ADTS did not recover")
	}
	a.SetAudioCodec("ac3")
	a.Push(packet(10, 0x0b, 0x77))
	if a.Snapshot().InvalidAudio != 0 {
		t.Fatal("valid Dolby framing rejected")
	}
	a.SetAudioCodec("aac_latm")
	a.Push(packet(12, 0x56, 0xe0))
	if a.Snapshot().InvalidAudio != 0 {
		t.Fatal("valid LATM framing rejected")
	}
}
