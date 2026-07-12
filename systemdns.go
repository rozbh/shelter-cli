package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
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

// setSystemDNS applies dns1/dns2 as the system's DNS servers.
// best-effort per OS — needs admin/root to actually take effect.
func setSystemDNS(dns1, dns2 string) error {
	logf("dns: setSystemDNS called with dns1=%s dns2=%s (os=%s)", dns1, dns2, runtime.GOOS)
	if !isElevated() {
		logf("dns: NOT elevated (euid check failed) — refusing to run resolvectl/networksetup/netsh")
		// bail out BEFORE calling resolvectl/networksetup: on linux those go
		// through polkit, which pops a password dialog for a non-root caller.
		// skipping here means "not root" fails silently/cleanly instead of
		// nagging for a password on every connect attempt and on exit.
		return fmt.Errorf("not running as root/admin — run the whole app with sudo to set dns")
	}
	logf("dns: elevated check passed, proceeding")

	switch runtime.GOOS {
	case "windows":
		return setDNSWindows(dns1, dns2)
	case "darwin":
		return setDNSMac(dns1, dns2)
	case "linux":
		return setDNSLinux(dns1, dns2)
	default:
		return fmt.Errorf("unsupported OS for DNS set: %s", runtime.GOOS)
	}
}

// ---- windows ----

var winIfaceRe = regexp.MustCompile(`(?i)^\s*\d+\s+\d+\s+\d+\s+connected\s+(.+?)\s*$`)

// findActiveWindowsInterface parses `netsh interface ipv4 show interfaces`
// and returns the name of the first "connected" interface.
func findActiveWindowsInterface() (string, error) {
	out, err := exec.Command("netsh", "interface", "ipv4", "show", "interfaces").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("netsh show interfaces: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if m := winIfaceRe.FindStringSubmatch(line); len(m) == 2 {
			iface := strings.TrimSpace(m[1])
			logf("dns(windows): detected active interface %q", iface)
			return iface, nil
		}
	}
	logf("dns(windows): no connected interface found in netsh output:\n%s", string(out))
	return "", fmt.Errorf("no connected interface found")
}

func setDNSWindows(dns1, dns2 string) error {
	iface, err := findActiveWindowsInterface()
	if err != nil {
		return err
	}

	logf("dns(windows): setting primary dns %s on %q", dns1, iface)
	if out, err := exec.Command("netsh", "interface", "ip", "set", "dns",
		"name="+iface, "static", dns1, "primary").CombinedOutput(); err != nil {
		logf("dns(windows): set primary FAILED: %v (%s)", err, string(out))
		return fmt.Errorf("set primary dns on %q: %w (%s)", iface, err, string(out))
	}

	logf("dns(windows): adding secondary dns %s on %q", dns2, iface)
	if out, err := exec.Command("netsh", "interface", "ip", "add", "dns",
		"name="+iface, dns2, "index=2").CombinedOutput(); err != nil {
		logf("dns(windows): add secondary FAILED: %v (%s)", err, string(out))
		return fmt.Errorf("add secondary dns on %q: %w (%s)", iface, err, string(out))
	}

	logf("dns(windows): both entries applied ok on %q", iface)
	return nil
}

// ---- macOS ----

func listMacNetworkServices() ([]string, error) {
	out, err := exec.Command("networksetup", "-listallnetworkservices").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("listallnetworkservices: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var services []string
	for i, l := range lines {
		if i == 0 {
			continue // header: "An asterisk (*) denotes that a network service is disabled."
		}
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "*") {
			continue // skip disabled services
		}
		services = append(services, l)
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no active network services found")
	}
	return services, nil
}

func setDNSMac(dns1, dns2 string) error {
	services, err := listMacNetworkServices()
	if err != nil {
		return err
	}
	logf("dns(mac): active services found: %v", services)

	// apply to every active service (usually just Wi-Fi or Ethernet is live,
	// but setting on all active ones is harmless and covers both cases)
	var lastErr error
	applied := 0
	for _, svc := range services {
		out, err := exec.Command("networksetup", "-setdnsservers", svc, dns1, dns2).CombinedOutput()
		if err != nil {
			logf("dns(mac): set on %q FAILED: %v (%s)", svc, err, string(out))
			lastErr = fmt.Errorf("set dns on %q: %w (%s)", svc, err, string(out))
			continue
		}
		logf("dns(mac): set on %q ok", svc)
		applied++
	}
	if applied == 0 {
		return lastErr
	}
	logf("dns(mac): applied to %d/%d services", applied, len(services))
	return nil
}

// ---- linux ----

// findDefaultLinuxInterface reads the default route to get the active iface name.
func findDefaultLinuxInterface() (string, error) {
	out, err := exec.Command("ip", "route", "show", "default").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ip route show default: %w", err)
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			iface := fields[i+1]
			logf("dns(linux): default interface is %q", iface)
			return iface, nil
		}
	}
	logf("dns(linux): no default interface found in: %s", strings.TrimSpace(string(out)))
	return "", fmt.Errorf("no default interface found")
}

func setDNSLinux(dns1, dns2 string) error {
	// preferred path: systemd-resolved. most modern distros (ubuntu, fedora,
	// arch, debian12+) run this and manage /etc/resolv.conf as a symlink to
	// their own stub file — writing resolv.conf directly on these systems
	// just gets overwritten and fights resolved, so only fall back to it
	// when resolvectl genuinely isn't present.
	if _, lookErr := exec.LookPath("resolvectl"); lookErr == nil {
		logf("dns(linux): resolvectl found on PATH, using it")
		iface, ierr := findDefaultLinuxInterface()
		if ierr != nil {
			return fmt.Errorf("resolvectl present but no default interface found: %w", ierr)
		}

		logf("dns(linux): running: resolvectl dns %s %s %s", iface, dns1, dns2)
		out, err := exec.Command("resolvectl", "dns", iface, dns1, dns2).CombinedOutput()
		if err != nil {
			logf("dns(linux): resolvectl dns FAILED: %v (%s)", err, strings.TrimSpace(string(out)))
			return fmt.Errorf("resolvectl dns %s %s %s failed (need root/sudo?): %w (%s)",
				iface, dns1, dns2, err, strings.TrimSpace(string(out)))
		}

		// verify it actually took
		verify, _ := exec.Command("resolvectl", "dns", iface).CombinedOutput()
		logf("dns(linux): readback after set: %s", strings.TrimSpace(string(verify)))
		return nil
	}

	// no resolvectl on this system at all: fall back to writing resolv.conf directly.
	logf("dns(linux): resolvectl not found, writing /etc/resolv.conf directly")
	content := fmt.Sprintf("nameserver %s\nnameserver %s\n", dns1, dns2)
	cmd := exec.Command("sh", "-c", fmt.Sprintf("printf '%s' > /etc/resolv.conf", content))
	if out, err := cmd.CombinedOutput(); err != nil {
		logf("dns(linux): writing /etc/resolv.conf FAILED: %v (%s)", err, strings.TrimSpace(string(out)))
		return fmt.Errorf("write /etc/resolv.conf (need root/sudo?): %w (%s)", err, strings.TrimSpace(string(out)))
	}
	logf("dns(linux): /etc/resolv.conf written ok")
	return nil
}
