package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"
)

// Docker calls this with the same listener configuration as the running server.
func healthcheck(address string) int {
	target, err := localHealthAddress(address)
	if err != nil {
		return 1
	}
	var dialer net.Dialer
	client := http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", target.String())
		}},
	}
	response, err := client.Get("http://localhost/healthz")
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	var ready struct{ Status, Stage string }
	if response.StatusCode == http.StatusOK && json.NewDecoder(response.Body).Decode(&ready) == nil &&
		ready.Status == "ok" && ready.Stage == "ready" {
		return 0
	}
	return 1
}

// A probe may contact only an address assigned to this container. A non-loopback
// address is necessary when a legacy deployment binds its listener to one interface.
func localHealthAddress(address string) (*net.TCPAddr, error) {
	target, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		return nil, err
	}
	if len(target.IP) == 0 || target.IP.IsUnspecified() {
		if target.IP.To4() != nil || len(target.IP) == 0 {
			target.IP = net.IPv4(127, 0, 0, 1)
		} else {
			target.IP = net.IPv6loopback
		}
	}
	if target.IP.IsLoopback() {
		return target, nil
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if local, ok := address.(*net.IPNet); ok && local.IP.Equal(target.IP) {
			return &net.TCPAddr{IP: local.IP, Port: target.Port, Zone: target.Zone}, nil
		}
	}
	return nil, errors.New("health check address is not assigned to this host")
}
