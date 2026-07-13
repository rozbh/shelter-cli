package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"shelter-cli/internal/config"
	"shelter-cli/internal/logging"
	"shelter-cli/internal/shelter"
)

type screen int

const (
	screenSetup screen = iota
	screenMain
)

// Model is the top-level bubbletea model for shelter-cli.
type Model struct {
	screen  screen
	cfg     config.Config
	inputs  []textinput.Model
	focused int
	saveErr string

	checks  []checkResult
	loading bool
	lastRun time.Time

	shelter shelter.Status // shelter connection status: disconnected/connecting/connected/failed
}

// NewModel builds the initial model: setup screen if no valid config
// exists yet, otherwise straight into the main connectivity screen.
func NewModel() Model {
	ip1 := textinput.New()
	ip1.Placeholder = "8.8.4.4"
	ip1.Focus()
	ip1.CharLimit = 64
	ip1.Width = 30

	ip2 := textinput.New()
	ip2.Placeholder = "9.9.9.9"
	ip2.CharLimit = 64
	ip2.Width = 30

	dnsIn := textinput.New()
	dnsIn.Placeholder = "google.com"
	dnsIn.CharLimit = 64
	dnsIn.Width = 30

	m := Model{
		screen: screenSetup,
		inputs: []textinput.Model{ip1, ip2, dnsIn},
	}

	// shelter status lives in memory only, for this run — always starts
	// disconnected. disk state from a previous run doesn't tell us whether
	// we're actually connected right now, so we don't trust it.
	m.shelter = shelter.Status{State: shelter.Disconnected}

	if cfg, ok := config.Load(); ok {
		m.cfg = cfg
		m.screen = screenMain
		m.loading = true
	}

	return m
}

func (m Model) Init() tea.Cmd {
	if m.screen == screenMain {
		return tea.Batch(checksCmd(m.cfg), tickCmd())
	}
	return textinput.Blink
}

func (m Model) allFilled() bool {
	for _, in := range m.inputs {
		if in.Value() == "" {
			return false
		}
	}
	return true
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenSetup:
		return m.updateSetup(msg)
	default:
		return m.updateMain(msg)
	}
}

func (m Model) updateSetup(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit

		case "tab", "shift+tab", "down", "up":
			if msg.String() == "tab" || msg.String() == "down" {
				m.focused = (m.focused + 1) % len(m.inputs)
			} else {
				m.focused = (m.focused - 1 + len(m.inputs)) % len(m.inputs)
			}
			for i := range m.inputs {
				if i == m.focused {
					m.inputs[i].Focus()
				} else {
					m.inputs[i].Blur()
				}
			}
			return m, nil

		case "enter":
			if m.allFilled() {
				dns1, dns2 := m.inputs[0].Value(), m.inputs[1].Value()
				if !config.ValidIP(dns1) || !config.ValidIP(dns2) {
					m.saveErr = "dns1/dns2 must be valid IP addresses"
					return m, nil
				}

				cfg := config.Config{
					DNS1:   m.inputs[0].Value(),
					DNS2:   m.inputs[1].Value(),
					DNSKey: m.inputs[2].Value(),
				}
				if err := config.Save(cfg); err != nil {
					m.saveErr = "cannot write config file: " + err.Error()
					return m, nil
				}
				m.saveErr = ""
				m.cfg = cfg
				m.screen = screenMain
				m.loading = true
				return m, tea.Batch(checksCmd(m.cfg), tickCmd())
			}
			// not all filled yet: move to next empty field
			m.focused = (m.focused + 1) % len(m.inputs)
			for i := range m.inputs {
				if i == m.focused {
					m.inputs[i].Focus()
				} else {
					m.inputs[i].Blur()
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	return m, cmd
}

func (m Model) updateMain(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case checksMsg:
		m.checks = []checkResult(msg)
		m.loading = false
		m.lastRun = time.Now()

		if !internetUp(m.checks) {
			logging.Logf("tui: internet is DOWN, marking shelter disconnected")
			m.shelter.State = shelter.Disconnected
			return m, nil
		}

		// network up but dns resolution broken -> current dns1/dns2 stopped
		// working (or got overwritten). don't trust "Connected" anymore, force
		// it back to Failed so the block below retries this same tick.
		if dnsBroken(m.checks) && m.shelter.State == shelter.Connected {
			logging.Logf("tui: dns checks failing while internet up, forcing shelter reconnect")
			m.shelter.State = shelter.Failed
		}
		// internet is up: if we're not already connected (or already trying),
		// kick off a connect attempt. covers first open + every retry on tick.
		if m.shelter.State != shelter.Connected && m.shelter.State != shelter.Connecting {
			logging.Logf("tui: internet up, shelter status=%s, triggering reconnect (reset dns + fetch ip + connect)", m.shelter.State)
			m.shelter.State = shelter.Connecting
			return m, resetAndConnectCmd(m.cfg)
		}
		return m, nil

	case shelterMsg:
		if msg.err != nil {
			logging.Logf("tui: shelter connect attempt finished with error: %v (final status=%s)", msg.err, msg.status.State)
		} else {
			logging.Logf("tui: shelter connect attempt finished OK (status=%s)", msg.status.State)
		}
		m.shelter = msg.status
		return m, nil

	case tickMsg:
		m.loading = true
		return m, tea.Batch(checksCmd(m.cfg), tickCmd())

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "r":
			m.loading = true
			return m, checksCmd(m.cfg)
		case "c":
			m.screen = screenSetup
			m.focused = 0
			for i := range m.inputs {
				m.inputs[i].SetValue("")
				if i == 0 {
					m.inputs[i].Focus()
				} else {
					m.inputs[i].Blur()
				}
			}
			return m, textinput.Blink
		}
	}
	return m, nil
}
