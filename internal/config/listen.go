package config

import "net"

// PrivateIPv4 returns a LAN address suitable for Plex discovery. A machine
// with several private networks can set the advertised address explicitly.
func PrivateIPv4() string {
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil && ip.To4() != nil && ip.IsPrivate() && !ip.IsLoopback() {
			return ip.String()
		}
	}
	return ""
}

func DesktopTunerListen() string {
	host := PrivateIPv4()
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, "9192")
}
