package dns

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// NewResolver returns a net.Resolver that queries server directly over udp:53,
// bypassing the OS resolver entirely. pure Go stdlib — identical behavior on
// linux/mac/windows, no system dns command involved.
func NewResolver(server string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", server+":53")
		},
	}
}

// NewHTTPClient returns an *http.Client whose outgoing requests resolve
// hostnames against server (via NewResolver) instead of the OS resolver.
//
// use this for ANY http call shelter-cli makes on its own behalf — panel
// fetch, register-ip, public-ip lookup. pass fallback (8.8.8.8/1.1.1.1)
// before a connect attempt, since system dns may be stale, mid-flap from a
// just-finished nmcli/resolvectl/netsh reactivate, or pointed at shelter's
// own dns1/dns2 which might be the very thing that's broken.
func NewHTTPClient(server string, timeout time.Duration) *http.Client {
	resolver := NewResolver(server)
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips, err := resolver.LookupHost(ctx, host)
				if err != nil {
					return nil, fmt.Errorf("resolve %s via %s: %w", host, server, err)
				}
				if len(ips) == 0 {
					return nil, fmt.Errorf("resolve %s via %s: no records", host, server)
				}
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
			},
		},
	}
}
