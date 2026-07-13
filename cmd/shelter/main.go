// Command shelter is a terminal connectivity monitor and shelter DNS manager.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"shelter-cli/internal/dns"
	"shelter-cli/internal/logging"
	"shelter-cli/internal/tui"
)

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
		logging.Logf("main: resetting dns to fallback %s/%s (exit/crash/signal path)", fallbackDNS1, fallbackDNS2)
		if err := dns.SetSystemDNS(dns.FallbackDNS1, dns.FallbackDNS2); err != nil {
			logging.Logf("main: fallback dns reset FAILED: %v", err)
			fmt.Fprintln(os.Stderr, "warning: could not reset dns to fallback:", err)
		} else {
			logging.Logf("main: fallback dns reset ok")
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

	p := tea.NewProgram(tui.NewModel())

	// catch ctrl+c / kill signals — Kill() lets bubbletea restore the
	// terminal properly and makes p.Run() return, instead of yanking
	// the process out from under it with os.Exit while it's still running.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		p.Kill()
	}()

	// catch unhandled panics so a crash still resets dns before dying
	defer func() {
		if r := recover(); r != nil {
			resetDNSToFallback()
			fmt.Fprintln(os.Stderr, "fatal:", r)
			os.Exit(1)
		}
	}()

	_, err := p.Run()

	// normal close (q/esc/ctrl+c/program ended) all reset dns here now
	resetDNSToFallback()

	if err != nil {
		fmt.Println("error running program:", err)
		os.Exit(1)
	}
}
