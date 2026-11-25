package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigIntKeyStringMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "collector.yaml")
	// Mix Helm-style quoted string keys ("4") and unquoted integer keys (6).
	data := `
collector_id: test-col
site: glemmen
defaults:
  unifi_vlan_names:
    "4": Server tilbehør
    6: Compute
sources:
  unifi:
    - name: ctrl
      url: https://unifi.example.com
      vlan_names:
        "10": Mgmt
        20: Storage
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if got := cfg.Defaults.UnifiVLANNames[4]; got != "Server tilbehør" {
		t.Fatalf("expected defaults vlan 4 = %q, got %q", "Server tilbehør", got)
	}
	if got := cfg.Defaults.UnifiVLANNames[6]; got != "Compute" {
		t.Fatalf("expected defaults vlan 6 = %q, got %q", "Compute", got)
	}
	if len(cfg.Sources.Unifi) != 1 {
		t.Fatalf("expected 1 unifi profile, got %d", len(cfg.Sources.Unifi))
	}
	if got := cfg.Sources.Unifi[0].VLANNames[10]; got != "Mgmt" {
		t.Fatalf("expected profile vlan 10 = %q, got %q", "Mgmt", got)
	}
	if got := cfg.Sources.Unifi[0].VLANNames[20]; got != "Storage" {
		t.Fatalf("expected profile vlan 20 = %q, got %q", "Storage", got)
	}
}
