// Package dns applies and verifies system DNS settings across
// Windows, macOS, and Linux.
package dns

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"shelter-cli/internal/logging"
)

const (
	FallbackDNS1 = "8.8.8.8"
	FallbackDNS2 = "1.1.1.1"
)

// isElevated reports whether we can change system DNS without the OS
// prompting for credentials (root on linux/mac). windows is left true here —
// netsh just fails with access-denied if not elevated, no polkit-style popup.
func isElevated() bool {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		return os.Geteuid() == 0
	}
	return true
}

// SetSystemDNS applies dns1/dns2 as the system's DNS servers.
// best-effort per OS — needs admin/root to actually take effect.
func SetSystemDNS(dns1, dns2 string) error {
	logging.Logf("dns: SetSystemDNS called with dns1=%s dns2=%s (os=%s)", dns1, dns2, runtime.GOOS)
	if !isElevated() {
		logging.Logf("dns: NOT elevated (euid check failed) — refusing to run resolvectl/networksetup/netsh")
		// bail out BEFORE calling resolvectl/networksetup: on linux those go
		// through polkit, which pops a password dialog for a non-root caller.
		// skipping here means "not root" fails silently/cleanly instead of
		// nagging for a password on every connect attempt and on exit.
		return fmt.Errorf("not running as root/admin — run the whole app with sudo to set dns")
	}
	logging.Logf("dns: elevated check passed, proceeding")

	var err error
	switch runtime.GOOS {
	case "windows":
		err = setDNSWindows(dns1, dns2)
	case "darwin":
		err = setDNSMac(dns1, dns2)
	case "linux":
		err = setDNSLinux(dns1, dns2)
	default:
		return fmt.Errorf("unsupported OS for DNS set: %s", runtime.GOOS)
	}
	if err != nil {
		return err
	}
	flushDNSCache()
	return nil

}

// ---- windows ----

// findDefaultWindowsInterface asks Windows routing table directly for the
// interface carrying the default route — not just "any connected" iface.
func findDefaultWindowsInterfaceIndex() (string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object -Property RouteMetric | Select-Object -First 1 -ExpandProperty InterfaceIndex)`).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("get-netroute default index: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	idx := strings.TrimSpace(string(out))
	if idx == "" {
		return "", fmt.Errorf("no default route interface index found")
	}
	logging.Logf("dns(windows): default-route interface index is %q", idx)
	return idx, nil
}

// setDNSWindows binds DNS directly to the adapter object via ifIndex,
// same "bind to real object, not name guess" fix applied on Linux (NM
// connection UUID) and macOS (hardware port -> service mapping).
func setDNSWindows(dns1, dns2 string) error {
	idx, err := findDefaultWindowsInterfaceIndex()
	if err != nil {
		return err
	}

	logging.Logf("dns(windows): setting dns %s,%s on interface index %s", dns1, dns2, idx)
	script := fmt.Sprintf(
		`Set-DnsClientServerAddress -InterfaceIndex %s -ServerAddresses ("%s","%s")`,
		idx, dns1, dns2,
	)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		logging.Logf("dns(windows): set FAILED: %v (%s)", err, string(out))
		return fmt.Errorf("set-dnsclientserveraddress ifidx %s: %w (%s)", idx, err, strings.TrimSpace(string(out)))
	}

	verify, _ := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`(Get-DnsClientServerAddress -InterfaceIndex %s -AddressFamily IPv4).ServerAddresses`, idx)).CombinedOutput()
	logging.Logf("dns(windows): readback after set: %s", strings.TrimSpace(string(verify)))

	return nil
}

// ---- macOS ----

// findDefaultMacInterface finds the BSD device (e.g. en0) actually carrying
// the default route, then maps it to a network service name so DNS gets set
// on the one connection actually in use — not every active service.
func findDefaultMacInterface() (string, error) {
	out, err := exec.Command("route", "-n", "get", "default").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("route -n get default: %w", err)
	}
	var device string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			device = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
			break
		}
	}
	if device == "" {
		return "", fmt.Errorf("no interface found in route -n get default output")
	}
	logging.Logf("dns(mac): default-route device is %q", device)

	// map device (en0) -> network service name (e.g. "Wi-Fi")
	hw, err := exec.Command("networksetup", "-listallhardwareports").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("listallhardwareports: %w", err)
	}
	lines := strings.Split(string(hw), "\n")
	var lastPort string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "Hardware Port:") {
			lastPort = strings.TrimSpace(strings.TrimPrefix(l, "Hardware Port:"))
		} else if strings.HasPrefix(l, "Device:") {
			dev := strings.TrimSpace(strings.TrimPrefix(l, "Device:"))
			if dev == device {
				logging.Logf("dns(mac): device %q maps to service %q", device, lastPort)
				return lastPort, nil
			}
		}
	}
	return "", fmt.Errorf("no network service found for device %q", device)
}

func setDNSMac(dns1, dns2 string) error {
	svc, err := findDefaultMacInterface()
	if err != nil {
		return err
	}
	logging.Logf("dns(mac): setting dns on active service %q only", svc)

	out, err := exec.Command("networksetup", "-setdnsservers", svc, dns1, dns2).CombinedOutput()
	if err != nil {
		logging.Logf("dns(mac): set on %q FAILED: %v (%s)", svc, err, string(out))
		return fmt.Errorf("set dns on %q: %w (%s)", svc, err, string(out))
	}
	logging.Logf("dns(mac): set on %q ok", svc)
	return nil
}

// ---- linux ----

// findDefaultLinuxInterface reads the default route to get the active iface name.
func findDefaultLinuxInterface() (string, error) {
	out, err := exec.Command("ip", "route", "get", "8.8.8.8").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ip route get 8.8.8.8: %w", err)
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			iface := fields[i+1]
			logging.Logf("dns(linux): outbound interface is %q", iface)
			return iface, nil
		}
	}
	logging.Logf("dns(linux): no dev found in: %s", strings.TrimSpace(string(out)))
	return "", fmt.Errorf("no outbound interface found")
}

// setDNSLinux sets DNS via NetworkManager (nmcli) when the interface is
// NM-managed, so the change survives DHCP renew/reconnect and shows up
// correctly in GUI network settings — falls back to resolvectl-only
// (runtime, can get overwritten by NM) if nmcli isn't available.
func setDNSLinux(dns1, dns2 string) error {
	iface, ierr := findDefaultLinuxInterface()
	if ierr != nil {
		return fmt.Errorf("no default interface found: %w", ierr)
	}

	if _, lookErr := exec.LookPath("nmcli"); lookErr == nil {
		if err := setDNSViaNetworkManager(iface, dns1, dns2); err == nil {
			return nil
		} else {
			logging.Logf("dns(linux): nmcli path failed, falling back to resolvectl: %v", err)
		}
	}

	return setDNSViaResolvectl(iface, dns1, dns2)
}

// setDNSViaNetworkManager finds the NM connection profile bound to iface,
// sets ipv4.dns directly on it, disables ipv4.ignore-auto-dns so NM stops
// re-pushing DHCP/router DNS, then reactivates the connection.
func setDNSViaNetworkManager(iface, dns1, dns2 string) error {
	out, err := exec.Command("nmcli", "-t", "-f", "GENERAL.CONNECTION", "device", "show", iface).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nmcli device show %q: %w (%s)", iface, err, strings.TrimSpace(string(out)))
	}
	line := strings.TrimSpace(string(out))
	conn := strings.TrimPrefix(line, "GENERAL.CONNECTION:")
	conn = strings.TrimSpace(conn)
	if conn == "" || conn == "--" {
		return fmt.Errorf("no NetworkManager connection bound to %q", iface)
	}
	logging.Logf("dns(linux): iface %q bound to NM connection %q", iface, conn)

	dnsVal := dns1 + " " + dns2
	if out, err := exec.Command("nmcli", "con", "mod", conn, "ipv4.dns", dnsVal, "ipv4.ignore-auto-dns", "yes").CombinedOutput(); err != nil {
		return fmt.Errorf("nmcli con mod %q: %w (%s)", conn, err, strings.TrimSpace(string(out)))
	}
	logging.Logf("dns(linux): set ipv4.dns=%q ignore-auto-dns=yes on connection %q", dnsVal, conn)

	if out, err := exec.Command("nmcli", "con", "up", conn).CombinedOutput(); err != nil {
		return fmt.Errorf("nmcli con up %q: %w (%s)", conn, err, strings.TrimSpace(string(out)))
	}
	logging.Logf("dns(linux): reactivated connection %q with new dns", conn)
	return nil
}

// setDNSViaResolvectl is the old runtime-only path — kept as fallback for
// systems without NetworkManager (e.g. plain systemd-networkd).
func setDNSViaResolvectl(iface, dns1, dns2 string) error {
	if _, lookErr := exec.LookPath("resolvectl"); lookErr != nil {
		content := fmt.Sprintf("nameserver %s\nnameserver %s\n", dns1, dns2)
		if err := os.WriteFile("/etc/resolv.conf", []byte(content), 0o644); err != nil {
			return fmt.Errorf("write /etc/resolv.conf (need root/sudo?): %w", err)
		}
		return nil
	}

	logging.Logf("dns(linux): running: resolvectl dns %s %s %s", iface, dns1, dns2)
	out, err := exec.Command("resolvectl", "dns", iface, dns1, dns2).CombinedOutput()
	if err != nil {
		return fmt.Errorf("resolvectl dns %s %s %s failed (need root/sudo?): %w (%s)",
			iface, dns1, dns2, err, strings.TrimSpace(string(out)))
	}
	verify, _ := exec.Command("resolvectl", "dns", iface).CombinedOutput()
	logging.Logf("dns(linux): readback after set: %s", strings.TrimSpace(string(verify)))
	return nil
}

func flushDNSCache() {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("ipconfig", "/flushdns")
	case "darwin":
		//cmd = exec.Command("dscacheutil", "-flushcache")
	case "linux":
		if _, err := exec.LookPath("resolvectl"); err == nil {
			//cmd = exec.Command("resolvectl", "flush-caches")
		} else {
			return // no systemd-resolved, nothing to flush
		}
	default:
		return
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		logging.Logf("dns: flush cache FAILED (%s): %v (%s)", runtime.GOOS, err, strings.TrimSpace(string(out)))
		return
	}
	logging.Logf("dns: flush cache ok (%s)", runtime.GOOS)
}
