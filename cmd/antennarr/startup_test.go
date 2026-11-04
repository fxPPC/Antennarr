package main

import (
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestStartupReason(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{{&os.PathError{Op: "mkdir", Path: "/data/config", Err: syscall.EACCES}, "permission denied"}, {syscall.EROFS, "read-only"}, {syscall.ENOSPC, "full"}, {&json.SyntaxError{}, "JSON"}} {
		if got := startupReason(tc.err); !strings.Contains(got, tc.want) {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}
