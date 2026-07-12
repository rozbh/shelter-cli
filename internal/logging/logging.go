// Package logging provides a process-wide timestamped logger that writes
// only to a persistent shelter.log file in the current working directory —
// never to stderr, so it doesn't interfere with the TUI on screen.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var logWriter io.Writer = io.Discard // fallback if file can't be opened: drop logs, don't spam stderr

func init() {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not resolve log file path:", err)
		return
	}
	path := filepath.Join(dir, "shelter.log")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not open log file, logs will be dropped:", err)
		return
	}

	// file only — the TUI owns the screen, so nothing should print to
	// stderr while it's running.
	logWriter = f
	Logf("==== shelter-cli started, logging to %s ====", path)
}

// Logf writes a timestamped debug line to shelter.log in the working
// directory. Review a past run any time with:
//
//	cat shelter.log
//	tail -f shelter.log   (while it's running)
func Logf(format string, args ...interface{}) {
	ts := time.Now().Format("15:04:05.000")
	fmt.Fprintf(logWriter, "[%s] "+format+"\n", append([]interface{}{ts}, args...)...)
}
