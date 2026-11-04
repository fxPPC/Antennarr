package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"syscall"
)

// Explain common startup failures without dumping configuration or credentials.
func startupReason(err error) string {
	var syntax *json.SyntaxError
	var value *json.UnmarshalTypeError
	switch {
	case errors.Is(err, os.ErrPermission):
		return "permission denied: the container UID/GID needs read/write access to the appdata directory and its existing contents"
	case errors.Is(err, syscall.EROFS):
		return "read-only filesystem: mount the appdata directory read/write"
	case errors.Is(err, syscall.ENOSPC):
		return "appdata filesystem is full"
	case errors.Is(err, syscall.ENOTDIR):
		return "an appdata path is a file instead of a directory"
	case errors.As(err, &syntax), errors.As(err, &value):
		return "invalid config/config.json: check JSON syntax and value types"
	default:
		return "configuration or storage could not be initialised; check appdata access and configuration values"
	}
}
func startupError(message, root string, err error) {
	slog.Error(message, "reason", startupReason(err), "data_directory", root, "uid", os.Getuid(), "gid", os.Getgid())
}
