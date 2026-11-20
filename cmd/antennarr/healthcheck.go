package main

import (
	"antennarr/internal/config"
	"encoding/json"
	"net"
	"net/http"
	"time"
)

// Docker calls this with the same listener configuration as the running server.
func healthcheck() int {
	root, err := dataDirectory()
	if err != nil {
		return 1
	}
	listeners, err := config.ReadListeners(root, false)
	if err != nil {
		return 1
	}
	address := listeners.Admin
	if address == "" {
		address = listeners.Primary
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return 1
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}

	client := http.Client{
		Transport: &http.Transport{},
		Timeout:   2 * time.Second,
	}
	response, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
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
