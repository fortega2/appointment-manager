package web

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// RealIPHeader carries the client address as the reverse proxy saw it.
const RealIPHeader = "X-Real-Ip"

// ClientIP resolves the address a request came from, preferring RealIPHeader
// over the TCP peer address, and reports whether either parsed.
//
// The header is trustworthy only because of how this is deployed: the container
// publishes no host ports (docker/docker-compose.yml), so the app is reachable
// only through Caddy, and the Caddyfile overwrites the header with the proxy's
// own view of the peer — a client cannot set it. That stops holding the moment
// Cloudflare proxies the zone, until the Caddyfile trusts Cloudflare's ranges
// and forwards {client_ip} rather than {remote_host}. See ADR 0009.
//
// It returns netip.Addr rather than a string because callers key rate-limit
// state on the result: an unparsed header would otherwise become a map key of
// whatever size and shape the sender chose.
func ClientIP(r *http.Request) (netip.Addr, bool) {
	if realIP := strings.TrimSpace(r.Header.Get(RealIPHeader)); realIP != "" {
		if addr, err := netip.ParseAddr(realIP); err == nil {
			return canonical(addr), true
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, false
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}

	return canonical(addr), true
}

// canonical strips the zone and the IPv4-in-IPv6 wrapper so one client cannot
// present itself as two distinct keys.
func canonical(addr netip.Addr) netip.Addr {
	return addr.Unmap().WithZone("")
}
