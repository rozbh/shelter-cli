package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const refreshInterval = 10 * time.Second

// fixed check targets — always the same, user never edits these.
const (
	fixedCheck1 = "8.8.8.8"
	fixedCheck2 = "1.1.1.1"
)

// ---- row types ----

type checkResult struct {
	Label   string // e.g. "Public IP", "Ping IP 1", "DNS"
	Target  string // e.g. "8.8.8.8", "1.1.1.1", "example.com"
	OK      bool
	Latency string // ping round-trip time in ms ("timeout" if no reply)
}

var rttRe = regexp.MustCompile(`time[=<]([0-9.]+)\s*ms`)

// pingTarget runs one ICMP ping and returns ok + round-trip latency string.
func pingTarget(host string) (bool, string) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "1", "-w", "10000", host)
	} else {
		cmd = exec.Command("ping", "-c", "1", "-W", "10", host)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, "timeout"
	}
	m := rttRe.FindStringSubmatch(string(out))
	if len(m) == 2 {
		if v, perr := strconv.ParseFloat(m[1], 64); perr == nil {
			return true, fmt.Sprintf("%.0fms", v)
		}
		return true, m[1] + "ms"
	}
	return false, "timeout"
}

// getPublicIP fetches public IP (no ping — plain HTTP lookup).
func getPublicIP() (string, error) {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.ipify.org?format=json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var data struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}
	return data.IP, nil
}

// config holds the values entered on the setup screen. Persisted to disk.
type config struct {
	DNS1   string `json:"dns1"`
	DNS2   string `json:"dns2"`
	DNSKey string `json:"dnskey"`
}

func configPath() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shelter_config.json"), nil
}

func loadConfig() (config, bool) {
	var cfg config
	path, err := configPath()
	if err != nil {
		return cfg, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, false
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, false
	}
	if cfg.DNS1 == "" || cfg.DNS2 == "" || cfg.DNSKey == "" {
		return cfg, false
	}
	return cfg, true
}

func saveConfig(cfg config) error {
	path, err := configPath()
	if err != nil {
		return fmt.Errorf("cannot resolve path: %w", err)
	}

	dir := filepath.Dir(path)
	probe := filepath.Join(dir, ".ipbox_write_test")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("no write permission in %s: %w", dir, err)
	}
	f.Close()
	os.Remove(probe)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	return nil
}

// runChecks fetches public IP + runs all pings concurrently, returns table rows in order.
func runChecks(cfg config) []checkResult {
	type job struct {
		label, target string
	}
	jobs := []job{
		{"Internet", fixedCheck1},
		{"Internet", fixedCheck2},
		{"DNS/Internet", "google.com"},
		{"DNS/Intranet", "soft98.ir"},
	}

	pingResults := make([]checkResult, len(jobs))
	done := make(chan struct{}, len(jobs))
	for i, j := range jobs {
		go func(i int, label, target string) {
			ok, lat := pingTarget(target)
			pingResults[i] = checkResult{Label: label, Target: target, OK: ok, Latency: lat}
			done <- struct{}{}
		}(i, j.label, j.target)
	}

	var ipRow checkResult
	ipDone := make(chan struct{})
	go func() {
		ip, err := getPublicIP()
		if err != nil {
			ipRow = checkResult{Label: "Public IP", Target: "N/A", OK: false, Latency: "-"}
		} else {
			ipRow = checkResult{Label: "Public IP", Target: ip, OK: true, Latency: "-"}
		}
		close(ipDone)
	}()

	for range jobs {
		<-done
	}
	<-ipDone

	return append([]checkResult{ipRow}, pingResults...)
}

// ---- bubbletea messages ----

type checksMsg []checkResult
type tickMsg time.Time
type shelterMsg struct {
	status shelterStatus
	err    error
}

func checksCmd(cfg config) tea.Cmd {
	return func() tea.Msg {
		return checksMsg(runChecks(cfg))
	}
}

// internetUp reports true if either fixed ping target answered.
func internetUp(checks []checkResult) bool {
	for _, c := range checks {
		if c.Label == "Internet" && c.OK {
			return true
		}
	}
	return false
}

// publicIPFrom pulls the "Public IP" row value out of checks, "" if not found/failed.
func publicIPFrom(checks []checkResult) string {
	for _, c := range checks {
		if c.Label == "Public IP" && c.OK {
			return c.Target
		}
	}
	return ""
}

// shelterConnectCmd runs the fetch-session + register-ip (+ set-dns on success) flow off the UI thread.
func shelterConnectCmd(ip, dnsKey, dns1, dns2 string) tea.Cmd {
	return func() tea.Msg {
		st, err := connectShelter(ip, dnsKey, dns1, dns2)
		return shelterMsg{status: st, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ---- app state ----

type screen int

const (
	screenSetup screen = iota
	screenMain
)

type model struct {
	screen  screen
	cfg     config
	inputs  []textinput.Model
	focused int
	saveErr string

	checks  []checkResult
	loading bool
	lastRun time.Time

	shelter shelterStatus // shelter-status: disconnected/connecting/connected/failed
}

func initialModel() model {
	ip1 := textinput.New()
	ip1.Placeholder = "8.8.4.4"
	ip1.Focus()
	ip1.CharLimit = 64
	ip1.Width = 30

	ip2 := textinput.New()
	ip2.Placeholder = "9.9.9.9"
	ip2.CharLimit = 64
	ip2.Width = 30

	dns := textinput.New()
	dns.Placeholder = "google.com"
	dns.CharLimit = 64
	dns.Width = 30

	m := model{
		screen: screenSetup,
		inputs: []textinput.Model{ip1, ip2, dns},
	}

	// shelter status lives in memory only, for this run — always starts
	// disconnected. disk state from a previous run doesn't tell us whether
	// we're actually connected right now, so we don't trust it.
	m.shelter = shelterStatus{Status: shelterDisconnected}

	if cfg, ok := loadConfig(); ok {
		m.cfg = cfg
		m.screen = screenMain
		m.loading = true
	}

	return m
}

func (m model) Init() tea.Cmd {
	if m.screen == screenMain {
		return tea.Batch(checksCmd(m.cfg), tickCmd())
	}
	return textinput.Blink
}

func (m model) allFilled() bool {
	for _, in := range m.inputs {
		if in.Value() == "" {
			return false
		}
	}
	return true
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenSetup:
		return m.updateSetup(msg)
	default:
		return m.updateMain(msg)
	}
}

func (m model) updateSetup(msg tea.Msg) (tea.Model, tea.Cmd) {
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
				cfg := config{
					DNS1:   m.inputs[0].Value(),
					DNS2:   m.inputs[1].Value(),
					DNSKey: m.inputs[2].Value(),
				}
				if err := saveConfig(cfg); err != nil {
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

func (m model) updateMain(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case checksMsg:
		m.checks = []checkResult(msg)
		m.loading = false
		m.lastRun = time.Now()

		if !internetUp(m.checks) {
			// no internet -> shelter can't be reached either
			logf("main: internet is DOWN, marking shelter disconnected")
			m.shelter.Status = shelterDisconnected
			return m, nil
		}

		// internet is up: if we're not already connected (or already trying),
		// kick off a connect attempt. covers first open + every retry on tick.
		if m.shelter.Status != shelterConnected && m.shelter.Status != shelterConnecting {
			ip := publicIPFrom(m.checks)
			if ip == "" {
				logf("main: internet up but no public ip resolved, marking shelter failed")
				m.shelter.Status = shelterFailed
				return m, nil
			}
			logf("main: internet up, shelter status=%s, triggering connect attempt (ip=%s)", m.shelter.Status, ip)
			m.shelter.Status = shelterConnecting
			return m, shelterConnectCmd(ip, m.cfg.DNSKey, m.cfg.DNS1, m.cfg.DNS2)
		}
		return m, nil

	case shelterMsg:
		if msg.err != nil {
			logf("main: shelter connect attempt finished with error: %v (final status=%s)", msg.err, msg.status.Status)
		} else {
			logf("main: shelter connect attempt finished OK (status=%s)", msg.status.Status)
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

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FF9C")).
			Align(lipgloss.Center)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00FF9C")).
			Padding(1, 3)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			Italic(true)

	okStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#00FF9C"))

	failStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF5F5F"))

	checkLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D7D7D")).
			Width(14)

	checkTargetStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Width(16)

	fieldLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D7D7D")).
			Width(10)
)

func (m model) View() string {
	if m.screen == screenSetup {
		return m.viewSetup()
	}
	return m.viewMain()
}

func (m model) viewSetup() string {
	labels := []string{"DNS 1", "DNS 2", "DNS Key"}
	lines := []string{
		titleStyle.Width(40).Render("SETUP"),
		"",
	}
	for i, in := range m.inputs {
		lines = append(lines, fieldLabelStyle.Render(labels[i]+":")+" "+in.View())
	}
	lines = append(lines, "")
	if m.saveErr != "" {
		lines = append(lines, failStyle.Render(m.saveErr))
	} else if m.allFilled() {
		lines = append(lines, okStyle.Render("enter to start"))
	} else {
		lines = append(lines, helpStyle.Render("fill all fields, tab to move, enter to continue"))
	}

	box := boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	help := helpStyle.Render("tab/↑/↓ move  ·  enter confirm  ·  esc quit")
	return lipgloss.JoinVertical(lipgloss.Center, "\n"+box, help+"\n")
}

// shelterStatusStyled renders shelter-status with color matching its state.
func shelterStatusStyled(st shelterState) string {
	switch st {
	case shelterConnected:
		return okStyle.Render("connected")
	case shelterConnecting:
		return valueStyle.Render("connecting...")
	case shelterFailed:
		return failStyle.Render("failed")
	default:
		return failStyle.Render("disconnected")
	}
}

// dnsVerifyStyled renders whether the actual dns resolve test passed.
func dnsVerifyStyled(verified bool) string {
	if verified {
		return okStyle.Render("ok (resolved)")
	}
	return failStyle.Render("failed")
}

func (m model) viewMain() string {
	var content string

	if m.loading && len(m.checks) == 0 {
		content = "running checks..."
	} else {
		lines := make([]string, 0, len(m.checks)+2)
		lines = append(lines, titleStyle.Width(46).Render("CONNECTIVITY"))
		lines = append(lines, "")
		for _, c := range m.checks {
			var status string
			if c.Label == "Public IP" {
				status = "    "
			} else if c.OK {
				status = okStyle.Render(" OK ")
			} else {
				status = failStyle.Render("FAIL")
			}
			lat := c.Latency
			if lat == "" {
				lat = "-"
			}
			line := checkLabelStyle.Render(c.Label) +
				checkTargetStyle.Render(c.Target) +
				status + "  " + valueStyle.Render(lat)
			lines = append(lines, line)
		}
		lines = append(lines, "")
		lines = append(lines, checkLabelStyle.Render("Shelter")+shelterStatusStyled(m.shelter.Status))
		if m.shelter.Status == shelterConnected {
			lines = append(lines, checkLabelStyle.Render("DNS check")+dnsVerifyStyled(m.shelter.DNSVerified))
		}

		if m.loading {
			lines = append(lines, "")
			lines = append(lines, okStyle.Render("● running checks..."))
		} else if !m.lastRun.IsZero() {
			lines = append(lines, "")
			lines = append(lines, helpStyle.Render("updated "+m.lastRun.Format("15:04:05")))
		}
		content = lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	box := boxStyle.Render(content)
	help := helpStyle.Render("auto-refresh every 10s  ·  r refresh now  ·  c reconfigure  ·  q quit")

	return lipgloss.JoinVertical(lipgloss.Center, "\n"+box, help+"\n")
}

// fallbackDNS1/2 are what we reset the system to on crash, kill, or normal close.
const (
	fallbackDNS1 = "8.8.8.8"
	fallbackDNS2 = "1.1.1.1"
)

var resetDNSOnce sync.Once

// resetDNSToFallback points system DNS back at 8.8.8.8/1.1.1.1.
// wrapped in sync.Once so panic-path + signal-path + normal-exit-path
// can't all fire it twice.
func resetDNSToFallback() {
	resetDNSOnce.Do(func() {
		logf("main: resetting dns to fallback %s/%s (exit/crash/signal path)", fallbackDNS1, fallbackDNS2)
		if err := setSystemDNS(fallbackDNS1, fallbackDNS2); err != nil {
			logf("main: fallback dns reset FAILED: %v", err)
			fmt.Fprintln(os.Stderr, "warning: could not reset dns to fallback:", err)
		} else {
			logf("main: fallback dns reset ok")
		}
	})
}

// warnIfNotElevated prints a heads-up if we're not root (linux/mac) — dns-set
// commands need it, and without it every connect attempt will fail at that step.
func warnIfNotElevated() {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		if os.Geteuid() != 0 {
			fmt.Fprintln(os.Stderr, "warning: not running as root — setting system DNS will fail. run with sudo.")
		}
	}
	// windows: no simple syscall-free check here; netsh will just fail with
	// access-denied in the dns error if not run as Administrator.
}

func main() {
	warnIfNotElevated()

	// catch ctrl+c / kill signals so a forced close still resets dns
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		resetDNSToFallback()
		os.Exit(1)
	}()

	// catch unhandled panics so a crash still resets dns before dying
	defer func() {
		if r := recover(); r != nil {
			resetDNSToFallback()
			fmt.Fprintln(os.Stderr, "fatal:", r)
			os.Exit(1)
		}
	}()

	p := tea.NewProgram(initialModel())
	_, err := p.Run()

	// normal close (q / esc / program ended on its own) also resets dns
	resetDNSToFallback()

	if err != nil {
		fmt.Println("error running program:", err)
		os.Exit(1)
	}
}
