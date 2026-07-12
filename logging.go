package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var logWriter io.Writer = os.Stderr // fallback if file can't be opened

func init() {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not resolve log file path:", err)
		return
	}
	path := filepath.Join(dir, "shelter.log")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not open log file, logging to stderr only:", err)
		return
	}

	// write to both stderr (live) and the file (persisted) so nothing
	// needs to be manually redirected to be captured.
	logWriter = io.MultiWriter(os.Stderr, f)
	logf("==== shelter-cli started, logging to %s ====", path)
}

// logf writes a timestamped debug line to stderr AND shelter.log in the
// working directory. review a past run any time with:
//
//	cat shelter.log
//	tail -f shelter.log   (while it's running)
func logf(format string, args ...interface{}) {
	ts := time.Now().Format("15:04:05.000")
	fmt.Fprintf(logWriter, "[%s] "+format+"\n", append([]interface{}{ts}, args...)...)
}
