package manual

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDirAndConfigMapSymlinks(t *testing.T) {
	root := t.TempDir()

	// Simulate Kubernetes ConfigMap ..data hidden dir alongside visible symlinks.
	hiddenDir := filepath.Join(root, "..2026_11_25_00_00_00.12345")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	siteYAML := `
schema_version: 1
defaults:
  site: hq
  owner: Platform Team
  tags: [prod]
sites:
  - id: hq
    name: HQ Room
owners:
  - name: Platform Team
device_types:
  - id: sw-24
    manufacturer: Ubiquiti
    model: USW-24
    u_height: 1
  - id: srv-2u
    manufacturer: Dell
    model: PowerEdge R515
    u_height: 2
racks:
  - name: Rack 1
    site: hq
    u_height: 10
    layout:
      - gap: 1
      - device: sw-01
        face: rear
      - gap: 2
      - device: srv-01
      - gap: 4
devices:
  - name: sw-01
    role: switch
    device_type: sw-24
    collect:
      snmp:
        ip: 10.0.0.2
        interfaces: true
        cables: true
  - name: srv-01
    role: server
    device_type: srv-2u
    uplinks:
      - interface: eno1
        switch: sw-01
        port: "1"
        type: cat6
`
	if err := os.WriteFile(filepath.Join(hiddenDir, "site.yaml"), []byte(siteYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(hiddenDir), filepath.Join(root, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", "site.yaml"), filepath.Join(root, "site.yaml")); err != nil {
		t.Fatal(err)
	}

	inv, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}
	if len(inv.FilesLoaded) != 1 {
		t.Fatalf("expected 1 deduplicated file, got %d (%v)", len(inv.FilesLoaded), inv.FilesLoaded)
	}
	if len(inv.Issues) != 0 {
		t.Fatalf("expected 0 issues, got %+v", inv.Issues)
	}
	if len(inv.Cables) != 1 {
		t.Fatalf("expected 1 expanded uplink cable, got %d", len(inv.Cables))
	}
	if inv.Cables[0].B.Port != "Port 1" {
		t.Fatalf("expected port expanded to 'Port 1', got %q", inv.Cables[0].B.Port)
	}

	plan := inv.RackPlans["hq/Rack 1"]
	swPos, ok := plan.Positions["sw-01"]
	if !ok || swPos.StartU != 9 || swPos.EndU != 9 || swPos.Face != "rear" {
		t.Fatalf("unexpected sw-01 placement: %+v (ok=%v)", swPos, ok)
	}
	srvPos, ok := plan.Positions["srv-01"]
	if !ok || srvPos.StartU != 5 || srvPos.EndU != 6 || srvPos.Face != "front" {
		t.Fatalf("unexpected srv-01 placement: %+v (ok=%v)", srvPos, ok)
	}

	hints := inv.BuildTargetHints("", "hq")
	if len(hints) != 1 || hints[0].SNMPIP != "10.0.0.2" {
		t.Fatalf("unexpected target hints: %+v", hints)
	}
}
