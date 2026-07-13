// Package config handles reading and writing the on-disk shelter-cli
// configuration (dns1, dns2, dnskey), collected once on the setup screen.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// Config holds the values entered on the setup screen. Persisted to disk.
type Config struct {
	DNS1   string `json:"dns1"`
	DNS2   string `json:"dns2"`
	DNSKey string `json:"dnskey"`
}

// Path returns the config file location. Uses os.Getwd() rather than
// os.Executable(): during `go run`, os.Executable() points at a temp
// build directory, which makes it unreliable for finding a stable config
// path across runs in development.
func Path() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shelter_config.json"), nil
}

// Load reads the config file, returning ok=false if it's missing,
// unreadable, or incomplete.
func Load() (Config, bool) {
	var cfg Config
	path, err := Path()
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

// Save writes cfg to disk, first probing that the directory is writable
// so failures surface as a clear error instead of a silent no-op.
func Save(cfg Config) error {
	path, err := Path()
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

// ValidIP reports whether s parses as an IP address.
func ValidIP(s string) bool {
	return net.ParseIP(s) != nil
}
