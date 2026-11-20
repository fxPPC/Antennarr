package config

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultPortRange = "39192-39198"

type Listeners struct {
	Range                  string
	Primary, Admin, Compat string
	Extra                  TunerPorts
}

func legacyPortEnvironment() bool {
	return os.Getenv("ANTENNARR_LISTEN") != "" || os.Getenv("ANTENNARR_ADMIN_LISTEN") != "" || os.Getenv("ANTENNARR_TUNER_PORTS") != ""
}

func freshPortRange() string {
	if legacyPortEnvironment() {
		return ""
	}
	return DefaultPortRange
}

// ReadListeners resolves the same addresses as the server without initialising any app data.
func ReadListeners(root string, desktop bool) (Listeners, error) {
	var saved struct {
		PortRange string `json:"port_range"`
	}
	raw, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		saved.PortRange = freshPortRange()
	case err != nil:
		return Listeners{}, err
	default:
		if err := json.Unmarshal(raw, &saved); err != nil {
			return Listeners{}, err
		}
	}
	return ResolveListeners(root, saved.PortRange, desktop)
}

func firstRangePort(value string) (int, error) {
	firstText, lastText, ok := strings.Cut(value, "-")
	first, err := strconv.Atoi(firstText)
	// Comparing canonical decimal strings also rejects signs, leading zeroes, and malformed endpoints.
	if !ok || err != nil || first < 1024 || first > 65529 || firstText != strconv.Itoa(first) || lastText != strconv.Itoa(first+6) {
		return 0, errors.New("port range must contain exactly seven consecutive ports between 1024 and 65535 (for example 39192-39198)")
	}
	return first, nil
}

func ResolveListeners(root, savedRange string, desktop bool) (Listeners, error) {
	requested := os.Getenv("ANTENNARR_PORT_RANGE")
	legacy := legacyPortEnvironment()
	if requested != "" && legacy {
		return Listeners{}, errors.New("ANTENNARR_PORT_RANGE conflicts with ANTENNARR_LISTEN, ANTENNARR_ADMIN_LISTEN, or ANTENNARR_TUNER_PORTS")
	}
	if requested == "" && !legacy {
		file, err := os.ReadFile(filepath.Join(root, "config", "port-range"))
		switch {
		case err == nil:
			requested = strings.TrimSpace(strings.TrimPrefix(string(file), "\ufeff"))
			if requested == "" {
				return Listeners{}, errors.New("empty config/port-range")
			}
		case errors.Is(err, os.ErrNotExist):
			requested = savedRange
		default:
			return Listeners{}, err
		}
	}
	n := Listeners{Range: requested, Compat: os.Getenv("ANTENNARR_COMPAT_LISTEN")}
	host, adminHost := "", ""
	if desktop {
		host = PrivateIPv4()
		if host == "" {
			host = "127.0.0.1"
		}
		adminHost = "127.0.0.1"
	}
	if requested != "" {
		first, err := firstRangePort(requested)
		if err != nil {
			return Listeners{}, err
		}
		n.Primary = net.JoinHostPort(host, strconv.Itoa(first+1))
		n.Admin = net.JoinHostPort(adminHost, strconv.Itoa(first))
		n.Extra = TunerPorts{first + 2, first + 6}
	} else {
		var err error
		n.Extra, err = ParseTunerPorts(os.Getenv("ANTENNARR_TUNER_PORTS"))
		if err != nil {
			return Listeners{}, err
		}
		n.Primary = os.Getenv("ANTENNARR_LISTEN")
		if n.Primary == "" {
			n.Primary = net.JoinHostPort(host, "9192")
		}
		n.Admin = os.Getenv("ANTENNARR_ADMIN_LISTEN")
		if n.Admin == "" && desktop {
			n.Admin = "127.0.0.1:39192"
		}
	}
	if err := n.Extra.ValidateListeners(n.Primary, n.Admin, n.Compat); err != nil {
		return Listeners{}, err
	}
	return n, nil
}

// The advertised host can differ from the bind interface under Docker or a reverse proxy.
func (n Listeners) BaseURL(override string, desktop bool) (string, error) {
	host, port, _ := net.SplitHostPort(n.Primary)
	if override != "" {
		if _, err := n.Extra.URL(override, 0); err != nil {
			return "", err
		}
		u, err := url.Parse(override)
		if err != nil {
			return "", err
		}
		if n.Range != "" && u.Port() == "" {
			u.Host = net.JoinHostPort(u.Hostname(), port)
			return strings.TrimSuffix(u.String(), "/"), nil
		}
		return strings.TrimSuffix(override, "/"), nil
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host = PrivateIPv4()
	}
	if host == "" && desktop {
		host = "127.0.0.1"
	}
	if host == "" {
		return "", errors.New("no private interface address for the tuner")
	}
	return "http://" + net.JoinHostPort(host, port), nil
}
