package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// TunerPorts reserves a small, explicit range for additional devices. The
// primary device keeps its existing URL/listener; offset 1 is the first extra
// port. Offsets belong to saved profiles and are never recycled on deletion.
// Keeping published and listening ports equal avoids advertising Docker's
// internal addresses to a Plex server on another host.
type TunerPorts struct{ First, Last int }

func ParseTunerPorts(value string) (TunerPorts, error) {
	if value == "" {
		value = "9200-9207"
	}
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return TunerPorts{}, fmt.Errorf("tuner ports must be a port or inclusive range")
	}
	first, err := strconv.Atoi(parts[0])
	if err != nil {
		return TunerPorts{}, fmt.Errorf("invalid first tuner port")
	}
	last := first
	if len(parts) == 2 {
		last, err = strconv.Atoi(parts[1])
		if err != nil {
			return TunerPorts{}, fmt.Errorf("invalid last tuner port")
		}
	}
	if first < 1024 || last > 65535 || last < first || last-first >= 64 {
		return TunerPorts{}, fmt.Errorf("tuner range must contain 1–64 ports between 1024 and 65535")
	}
	return TunerPorts{first, last}, nil
}

func (p TunerPorts) Port(offset int) (int, error) {
	if offset < 1 || offset > p.Last-p.First+1 {
		return 0, fmt.Errorf("tuner port range is full; expand ANTENNARR_TUNER_PORTS and its private Docker mappings")
	}
	return p.First + offset - 1, nil
}

func (p TunerPorts) URL(primary string, offset int) (string, error) {
	u, err := url.Parse(primary)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("tuner base URL must be an HTTP(S) host address without credentials, query, or path")
	}
	if offset == 0 {
		return strings.TrimRight(primary, "/"), nil
	}
	port, err := p.Port(offset)
	if err != nil {
		return "", err
	}
	// Extra listeners speak HTTP, like the primary private tuner. The public
	// administration proxy must never be used as their advertised host.
	u.Scheme = "http"
	u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	u.Path = ""
	return u.String(), nil
}

func (p TunerPorts) ValidateListeners(addresses ...string) error {
	for _, address := range addresses {
		if address == "" {
			continue
		}
		_, raw, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("invalid listener address")
		}
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("invalid listener port")
		}
		if port >= p.First && port <= p.Last {
			return fmt.Errorf("additional tuner range overlaps another application listener")
		}
	}
	return nil
}
