//go:build windows

package main

import (
	"syscall"

	"shelter-cli/internal/logging"
)

// windows console close (X button), logoff, shutdown send
// CTRL_CLOSE_EVENT/CTRL_LOGOFF_EVENT/CTRL_SHUTDOWN_EVENT — NOT
// os.Interrupt/SIGTERM. go's signal.Notify never sees these, so the
// normal p.Kill() path in main() doesn't fire and dns never gets reset.
// SetConsoleCtrlHandler hooks these events directly at the OS level.
// windows gives handler ~5s (up to ~10s more if it returns TRUE meaning
// "handled") before force-killing — enough time to run netsh.
var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")
)

const (
	ctrlCloseEvent    = 2
	ctrlLogoffEvent   = 5
	ctrlShutdownEvent = 6
)

// installWindowsCtrlHandler registers a handler that runs resetFn on
// close/logoff/shutdown console events before windows kills the process.
func installWindowsCtrlHandler(resetFn func()) {
	handler := func(ctrlType uint32) uintptr {
		switch ctrlType {
		case ctrlCloseEvent, ctrlLogoffEvent, ctrlShutdownEvent:
			logging.Logf("main(windows): console ctrl event %d received, resetting dns before exit", ctrlType)
			resetFn()
			return 1 // TRUE: we handled it, don't run default handler yet
		}
		return 0
	}

	r, _, err := procSetConsoleCtrlHandler.Call(
		syscall.NewCallback(handler),
		1, // TRUE: add handler
	)
	if r == 0 {
		logging.Logf("main(windows): SetConsoleCtrlHandler failed: %v", err)
	} else {
		logging.Logf("main(windows): console ctrl handler installed")
	}
}
