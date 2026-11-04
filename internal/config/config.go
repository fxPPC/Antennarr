package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Channel struct {
	ID      string `json:"id"`
	Number  string `json:"number"`
	Name    string `json:"name"`
	Fixture string `json:"fixture,omitempty"`
}
type Config struct {
	FormatVersion int                        `json:"format_version"`
	ProfileID     string                     `json:"profile_id"`
	Extensions    map[string]json.RawMessage `json:"extensions,omitempty"`
	Settings      Settings                   `json:"settings"`
	DeviceID      string                     `json:"device_id"`
	Tuners        int                        `json:"tuners"`
	GraceSeconds  int                        `json:"grace_seconds"`
	Channels      []Channel                  `json:"channels"`
}

func Load(root string) (Config, error) {
	var c Config
	for _, d := range []string{"config", "database", "sources/imports", "secrets", "logs", "backups"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0700); err != nil {
			return c, err
		}
	}
	path := filepath.Join(root, "config/config.json")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		raw := make([]byte, 4)
		if _, err = rand.Read(raw); err != nil {
			return c, err
		}
		// SiliconDust device-ID checksum: XOR alternating lookup-transformed nibbles.
		raw[0] = 0x12
		id := hex.EncodeToString(raw)[:7]
		lut := []byte{10, 5, 15, 6, 7, 12, 1, 11, 9, 2, 8, 13, 4, 3, 14, 0}
		var sum byte
		for i, v := range id {
			var n byte
			if v >= 'a' {
				n = byte(v - 'a' + 10)
			} else {
				n = byte(v - '0')
			}
			if i%2 == 0 {
				sum ^= lut[n]
			} else {
				sum ^= n
			}
		}
		c = Config{Settings: FreshSettings(), FormatVersion: 1, ProfileID: "primary", DeviceID: fmt.Sprintf("%s%x", id, sum), Tuners: 3, GraceSeconds: 5, Channels: []Channel{
			{"test-1", "901", "Antennarr Test 720p", "720"},
			{"test-2", "902", "Antennarr Test 480p", "480"},
			{"test-3", "903", "Antennarr Test MPEG-2", "mpeg2"},
		}}
		b, _ = json.MarshalIndent(c, "", "  ")
		err = os.WriteFile(path, b, 0600)
	} else if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err != nil {
		return c, err
	}
	if c.FormatVersion == 0 {
		c.FormatVersion = 1
	}
	if c.ProfileID == "" {
		c.ProfileID = "primary"
	}
	c.Settings.Defaults()
	if e := c.Settings.Validate(); e != nil {
		return c, e
	}
	return c, Validate(c)
}
func Validate(c Config) error {
	if c.FormatVersion > 1 || c.FormatVersion < 0 {
		return fmt.Errorf("configuration requires a newer application")
	}
	if c.ProfileID != "" && c.ProfileID != "primary" {
		return fmt.Errorf("v1 supports only the primary output profile")
	}
	if len(c.DeviceID) != 8 {
		return fmt.Errorf("invalid device identity")
	}
	if _, err := hex.DecodeString(c.DeviceID); err != nil {
		return fmt.Errorf("invalid device identity")
	}
	if c.Tuners < 1 || c.Tuners > 3 || c.GraceSeconds < 0 || c.GraceSeconds > 60 {
		return fmt.Errorf("invalid tuner limits")
	}
	ids := map[string]bool{}
	nums := map[string]bool{}
	for _, ch := range c.Channels {
		if ch.ID == "" || ch.Name == "" || ids[ch.ID] || nums[ch.Number] {
			return fmt.Errorf("duplicate or invalid channel")
		}
		if ch.Number == "" {
			return fmt.Errorf("empty channel number")
		}
		for _, r := range ch.Number {
			if (r < '0' || r > '9') && r != '.' {
				return fmt.Errorf("invalid channel number")
			}
		}
		if ch.Fixture != "720" && ch.Fixture != "480" && ch.Fixture != "mpeg2" {
			return fmt.Errorf("unknown fixture")
		}
		ids[ch.ID] = true
		nums[ch.Number] = true
	}
	return nil
}
func AdminPassword(root string) (string, error) {
	p := filepath.Join(root, "secrets/admin-password")
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		b = []byte("admin")
		err = os.WriteFile(p, b, 0600)
	}
	return string(b), err
}
