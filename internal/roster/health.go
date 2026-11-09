package roster

import (
	"errors"
	"log/slog"
	"time"
)

// CheckerState contains only safe operational explanations, never source URLs.
type CheckerState struct {
	Mode      string `json:"mode"`
	State     string `json:"state"`
	Updated   int64  `json:"updated"`
	Next      int64  `json:"next"`
	LastError string `json:"last_error,omitempty"`
	ErrorAt   int64  `json:"error_at,omitempty"`
}

func (s *Service) CheckerState() CheckerState { s.mu.Lock(); defer s.mu.Unlock(); return s.health }
func (s *Service) state(state string, next int64) {
	s.mu.Lock()
	s.health.State = state
	s.health.Mode = "background"
	if s.replenishing {
		s.health.Mode = "live_replenishment"
	}
	s.health.Next = next
	s.health.Updated = time.Now().Unix()
	waiting := state == "checks_waiting" || state == "rosters_full" || state == "playback_priority" || state == "paused"
	logChange := waiting && (state != s.loggedWaitState || next != s.loggedWaitNext)
	if logChange {
		s.loggedWaitState = state
		s.loggedWaitNext = next
	}
	mode := s.health.Mode
	s.mu.Unlock()
	// Selection is transient on every tick. Log changed waits only, so idle
	// periods remain diagnosable without filling Docker's bounded log files.
	if logChange {
		retry := ""
		if next > 0 {
			retry = time.Unix(next, 0).UTC().Format(time.RFC3339)
		}
		slog.Debug("checker waiting", "state", state, "mode", mode, "retry_at", retry)
	}
}

func (s *Service) report(operation string, err error) {
	if err == nil {
		s.mu.Lock()
		if s.health.LastError == operation {
			s.health.LastError = ""
			s.health.ErrorAt = 0
		}
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	s.health.LastError = operation
	s.health.ErrorAt = time.Now().Unix()
	s.mu.Unlock()
	// Database error codes are safe diagnostics; raw errors from network work
	// can contain credential-bearing URLs and must never be logged here.
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		slog.Warn("checker operation failed", "operation", operation, "database_code", coded.Code())
	} else {
		slog.Warn("checker operation failed", "operation", operation)
	}
}
