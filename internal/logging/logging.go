package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const maxLogSize = 5 * 1024 * 1024 // 5MB

var logWriter io.Writer = io.Discard

func init() {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not resolve log file path:", err)
		return
	}
	path := filepath.Join(dir, "shelter.log")

	rotateIfLarge(path)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not open log file, logs will be dropped:", err)
		return
	}

	logWriter = f
	Logf("==== shelter-cli started, logging to %s ====", path)
}

// rotateIfLarge renames shelter.log -> shelter.log.old (overwriting any
// previous .old) if it's grown past maxLogSize, keeping disk use bounded.
func rotateIfLarge(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxLogSize {
		return
	}
	old := path + ".old"
	_ = os.Remove(old)
	_ = os.Rename(path, old)
}

func Logf(format string, args ...interface{}) {
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	fmt.Fprintf(logWriter, "[%s] "+format+"\n", append([]interface{}{ts}, args...)...)
}
