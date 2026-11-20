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
