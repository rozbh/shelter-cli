package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidIP(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"::1", true},
		{"", false},
		{"notanip", false},
		{"999.999.999.999", false},
		{"8.8.8.8; rm -rf /", false},
	}
	for _, c := range cases {
		if got := ValidIP(c.in); got != c.want {
			t.Errorf("ValidIP(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	os.Chdir(dir)

	cfg := Config{DNS1: "8.8.8.8", DNS2: "1.1.1.1", DNSKey: "testkey"}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, ok := Load()
	if !ok {
		t.Fatal("Load returned ok=false after Save")
	}
	if got != cfg {
		t.Errorf("Load() = %+v, want %+v", got, cfg)
	}

	if _, err := os.Stat(filepath.Join(dir, "shelter_config.json")); err != nil {
		t.Errorf("config file not written: %v", err)
	}
}

func TestLoadIncomplete(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	os.Chdir(dir)

	os.WriteFile("shelter_config.json", []byte(`{"dns1":"8.8.8.8"}`), 0o644)
	_, ok := Load()
	if ok {
		t.Error("Load should return ok=false when dns2/dnskey missing")
	}
}
