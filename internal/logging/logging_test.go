package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotateIfLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shelter.log")

	big := strings.Repeat("x", maxLogSize+1)
	os.WriteFile(path, []byte(big), 0o644)

	rotateIfLarge(path)

	if _, err := os.Stat(path + ".old"); err != nil {
		t.Fatalf("expected .old file after rotation: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("expected original path removed/renamed after rotation")
	}
}

func TestRotateIfSmall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shelter.log")
	os.WriteFile(path, []byte("small"), 0o644)

	rotateIfLarge(path)

	if _, err := os.Stat(path + ".old"); err == nil {
		t.Error("small file should not be rotated")
	}
}
