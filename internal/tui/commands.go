package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"shelter-cli/internal/config"
	"shelter-cli/internal/shelter"
)

const refreshInterval = 10 * time.Second

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

// shelterConnectCmd runs the fetch-session + register-ip (+ set-dns on success) flow off the UI thread.
func shelterConnectCmd(ip, dnsKey, dns1, dns2 string) tea.Cmd {
	return func() tea.Msg {
		st, err := shelter.Connect(ip, dnsKey, dns1, dns2)
		return shelterMsg{status: st, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
