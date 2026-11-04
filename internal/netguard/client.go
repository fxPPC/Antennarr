// Package netguard pins each dial to an approved public IP and rechecks redirects.
package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var blocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
}

func Public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range blocked {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
func URL(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.Opaque != "" || u.Fragment != "" {
		return errors.New("URL not permitted")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return errors.New("local destination blocked")
	}
	if ip, e := netip.ParseAddr(host); e == nil && !Public(ip) {
		return errors.New("non-public destination blocked")
	}
	return nil
}
func Client(timeout time.Duration) *http.Client {
	d := net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	t := &http.Transport{Proxy: nil, MaxIdleConns: 32, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 10 * time.Second, ForceAttemptHTTP2: true}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, errors.New("invalid destination")
		}
		ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if e != nil || len(ips) == 0 {
			return nil, errors.New("DNS lookup failed")
		}
		for _, ip := range ips {
			if !Public(ip) {
				return nil, errors.New("non-public DNS destination blocked")
			}
		}
		for _, ip := range ips {
			conn, e := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, errors.New("source connection failed")
	}
	return &http.Client{Transport: t, Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		if e := URL(r.URL); e != nil {
			return e
		}
		if len(via) > 0 && r.URL.Host != via[len(via)-1].URL.Host {
			r.Header.Del("Authorization")
			r.Header.Del("Cookie")
		}
		return nil
	}}
}
func Request(ctx context.Context, client *http.Client, method, raw string) (*http.Response, error) {
	u, e := url.Parse(raw)
	if e != nil || URL(u) != nil {
		return nil, errors.New("source URL blocked")
	}
	r, e := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if e != nil {
		return nil, errors.New("invalid source request")
	}
	r.Header.Set("User-Agent", "Antennarr/0.1 (authorised source client)")
	response, e := client.Do(r)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if IsTimeout(e) {
			return nil, context.DeadlineExceeded
		}
		var deferred interface {
			error
			TemporaryCheck() bool
		}
		if errors.As(e, &deferred) {
			return nil, deferred
		}
		return nil, errors.New("source request failed")
	}
	return response, nil
}
