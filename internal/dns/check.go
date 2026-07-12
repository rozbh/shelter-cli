package dns

import (
	"context"
	"fmt"
	"net"
	"time"

	"shelter-cli/internal/logging"
)

// VerifyDNS queries dns1 (then dns2 as fallback) directly for a known host,
// bypassing the OS resolver entirely — proves the DNS server itself answers,
// independent of whether the system-wide netsh/resolvectl change took hold.
func VerifyDNS(dns1, dns2 string) (ok bool, detail string) {
	testHost := "google.com"
	logging.Logf("dns-verify: querying %s directly against %s", testHost, dns1)

	if ok, detail := queryAgainst(dns1, testHost); ok {
		logging.Logf("dns-verify: dns1 (%s) answered: %s", dns1, detail)
		return true, fmt.Sprintf("resolved via %s: %s", dns1, detail)
	} else if dns2 != "" {
		logging.Logf("dns-verify: dns1 (%s) failed (%s), trying dns2 (%s)", dns1, detail, dns2)
		if ok2, detail2 := queryAgainst(dns2, testHost); ok2 {
			logging.Logf("dns-verify: dns2 (%s) answered: %s", dns2, detail2)
			return true, fmt.Sprintf("resolved via %s (dns1 failed): %s", dns2, detail2)
		} else {
			logging.Logf("dns-verify: both dns1 and dns2 failed")
			return false, fmt.Sprintf("both failed — dns1(%s): %s | dns2(%s): %s", dns1, detail, dns2, detail2)
		}
	}
	logging.Logf("dns-verify: dns1 failed and no dns2 configured")
	return false, fmt.Sprintf("dns1(%s) failed: %s", dns1, detail)
}

func queryAgainst(server, host string) (bool, string) {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", server+":53")
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	ips, err := resolver.LookupHost(ctx, host)
	if err != nil {
		return false, err.Error()
	}
	if len(ips) == 0 {
		return false, "no records returned"
	}
	return true, fmt.Sprintf("%s in %s", ips[0], time.Since(start).Round(time.Millisecond))
}
