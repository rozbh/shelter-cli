package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"shelter-cli/internal/config"
	"shelter-cli/internal/dns"
	"shelter-cli/internal/shelter"
)

const refreshInterval = 10 * time.Second

// known-good public resolver — used to un-stick ip-fetch/panel calls before
// they'd otherwise depend on system dns propagation timing. same values as
// cmd/shelter/main.go's crash-reset fallback.
const (
	fallbackDNS1 = "8.8.8.8"
	fallbackDNS2 = "1.1.1.1"
)

type checksMsg []checkResult
type tickMsg time.Time
type shelterMsg struct {
	status shelter.Status
	err    error
}

func checksCmd(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		return checksMsg(runChecks(cfg))
	}
}

// resetAndConnectCmd: reset system dns to fallback -> fetch public ip via
// fallback (not system resolver) -> shelter.Connect (which itself does all
// panel traffic via fallback dns, then switches system dns to dns1/dns2 and
// verifies). single entrypoint for first-connect, retry-on-fail, ip-change,
// and dns-broken-while-connected — all funnel through here.
func resetAndConnectCmd(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		if err := dns.SetSystemDNS(fallbackDNS1, fallbackDNS2); err != nil {
			// not fatal, keep trying
		}

		ip, err := getPublicIP(fallbackDNS1)
		if err != nil || ip == "" {
			return shelterMsg{
				status: shelter.Status{State: shelter.Failed, UpdatedAt: time.Now()},
				err:    fmt.Errorf("could not fetch public ip after dns reset: %v", err),
			}
		}

		st, cerr := shelter.Connect(ip, cfg.DNSKey, cfg.DNS1, cfg.DNS2)
		return shelterMsg{status: st, err: cerr}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
