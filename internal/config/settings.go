package config

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
)

type Settings struct {
	ReplenishWhilePlaying    bool   `json:"replenish_while_playing"`
	LoggingLevel             string `json:"logging_level"`
	AllowUHD                 bool   `json:"allow_uhd"`
	RosterTarget             int    `json:"roster_target"`
	SelectionPreference      string `json:"selection_preference"`
	MaxOutgoingStreams       int    `json:"max_outgoing_streams"`
	StartupRace              int    `json:"startup_race"`
	GlobalConnections        int    `json:"global_connections"`
	ProviderConnections      int    `json:"provider_connections"`
	BufferSeconds            int    `json:"buffer_seconds"`
	StallTimeoutMS           int    `json:"stall_timeout_ms"`
	ProbeSeconds             int    `json:"probe_seconds"`
	CheckAPISeconds          int    `json:"check_api_seconds"`
	CheckGlobalSeconds       int    `json:"check_global_seconds"`
	CheckStreamSeconds       int    `json:"check_stream_seconds"`
	CheckStreamGlobalSeconds int    `json:"check_stream_global_seconds"`
	CatalogueHours           int    `json:"catalogue_hours"`
}

// FreshSettings is applied only when config.json is absent. Defaults below
// remains the compatibility fallback for fields absent from older saved files.
func FreshSettings() Settings {
	return Settings{
		LoggingLevel: "debug", RosterTarget: 20, SelectionPreference: "stability", MaxOutgoingStreams: 4,
		StartupRace: 5, GlobalConnections: 15, ProviderConnections: 3,
		BufferSeconds: 3, StallTimeoutMS: 1500, ProbeSeconds: 5,
		CheckAPISeconds: 33, CheckGlobalSeconds: 2, CheckStreamSeconds: 63,
		CheckStreamGlobalSeconds: 6, CatalogueHours: 144,
	}
}

func (s *Settings) Defaults() {
	if s.LoggingLevel == "" {
		s.LoggingLevel = "debug"
	}
	if s.RosterTarget == 0 {
		s.RosterTarget = 30
	}
	if s.SelectionPreference == "" {
		s.SelectionPreference = "stability"
	}
	if s.MaxOutgoingStreams == 0 {
		s.MaxOutgoingStreams = 3
	}
	if s.CheckAPISeconds == 0 {
		s.CheckAPISeconds = 10
	}
	if s.CheckGlobalSeconds == 0 {
		s.CheckGlobalSeconds = 2
	}
	if s.CheckStreamSeconds == 0 {
		s.CheckStreamSeconds = 60
	}
	if s.CheckStreamGlobalSeconds == 0 {
		s.CheckStreamGlobalSeconds = 10
	}
	if s.CatalogueHours == 0 {
		s.CatalogueHours = 24
	}
	if s.StartupRace == 0 {
		s.StartupRace = 5
	}
	if s.GlobalConnections == 0 {
		s.GlobalConnections = 10
	}
	if s.ProviderConnections == 0 {
		s.ProviderConnections = 5
	}
	if s.BufferSeconds == 0 {
		s.BufferSeconds = 3
	}
	if s.StallTimeoutMS == 0 {
		s.StallTimeoutMS = 1500
	}
	if s.ProbeSeconds == 0 {
		s.ProbeSeconds = 4
	}
}
func (s Settings) Validate() error {
	if s.LoggingLevel != "debug" && s.LoggingLevel != "normal" && s.LoggingLevel != "none" {
		return errors.New("invalid logging level")
	}
	if s.RosterTarget < 2 || s.RosterTarget > 200 || (s.SelectionPreference != "stability" && s.SelectionPreference != "diversity") || s.MaxOutgoingStreams < 1 || s.MaxOutgoingStreams > 64 || s.StartupRace < 2 || s.StartupRace > 10 || s.GlobalConnections < 6 || s.GlobalConnections > 256 || s.ProviderConnections < 1 || s.ProviderConnections > s.GlobalConnections || s.BufferSeconds < 1 || s.BufferSeconds > 10 || s.StallTimeoutMS < 500 || s.StallTimeoutMS > 10000 || s.ProbeSeconds < 3 || s.ProbeSeconds > 10 || s.CheckGlobalSeconds < 1 || s.CheckGlobalSeconds > 60 || s.CheckAPISeconds < s.CheckGlobalSeconds || s.CheckAPISeconds > 600 || s.CheckStreamGlobalSeconds < 5 || s.CheckStreamGlobalSeconds > 60 || s.CheckStreamSeconds < s.CheckStreamGlobalSeconds || s.CheckStreamSeconds > 3600 || s.CatalogueHours < 12 || s.CatalogueHours > 168 {
		return errors.New("settings outside supported ranges")
	}
	return nil
}
func Save(root string, c Config) error {
	if e := Validate(c); e != nil {
		return e
	}
	if e := c.Settings.Validate(); e != nil {
		return e
	}
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(filepath.Join(root, "config"), "config-*.tmp")
	if e != nil {
		return e
	}
	path := tmp.Name()
	defer os.Remove(path)
	if e = tmp.Chmod(0600); e == nil {
		_, e = tmp.Write(b)
	}
	if e == nil {
		e = tmp.Sync()
	}
	closeErr := tmp.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path, filepath.Join(root, "config/config.json"))
}

// Increasing viewers should also make room for distinct-channel hot standbys.
// Preserve an explicitly changed upstream cap and never shrink existing capacity.
func AdjustViewerCapacity(next, previous Settings) Settings {
	if next.MaxOutgoingStreams > previous.MaxOutgoingStreams && next.GlobalConnections == previous.GlobalConnections {
		needed := 2*next.MaxOutgoingStreams + max(0, next.StartupRace-2)
		next.GlobalConnections = max(next.GlobalConnections, needed)
	}
	return next
}

// The shared LevelVar applies changes immediately to every package's Docker logs.
func (s Settings) LogLevel() slog.Level {
	switch s.LoggingLevel {
	case "none":
		return slog.Level(127)
	case "normal":
		return slog.LevelWarn
	default:
		return slog.LevelDebug
	}
}
