package merge

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/blalange/auto-netviz/internal/db"
	"github.com/blalange/auto-netviz/internal/manual"
	"github.com/blalange/auto-netviz/internal/protocol"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAutoCreateAndCrossCollectorCorrelation(t *testing.T) {
	now := time.Now().UTC()
	inv := &manual.Inventory{
		Defaults: manual.Defaults{Site: "hq", Owner: "Platform"},
		Rules: manual.EffectiveRules{
			AutoNetworkDevices: true,
			AutoServers:        true,
			AutoWiredClients:   true,
		},
		RackPlans: make(map[string]manual.RackPlan),
	}

	// Collector 1 (OOB VLAN) sees Redfish BMC; Collector 2 (Compute VLAN) sees HostOS Linux
	// sharing the same system_uuid -> must auto-create ONE unified device!
	uuid := "4c4c4544-0054-4210-8037-b8c04f4a3333"
	rawObs := []db.StoredObservation{
		{
			ID: 1, CollectorID: "col-oob", Site: "hq", Source: "redfish", Profile: "default", Kind: "device", ObsKey: "10.10.4.55",
			ObservedAt: now,
			PayloadJSON: mustJSON(t, protocol.DeviceObservation{
				ObsKey:       "10.10.4.55",
				Site:         "hq",
				Name:         "srv-auto-55",
				RoleHint:     "server",
				Manufacturer: "Dell",
				Model:        "PowerEdge R650",
				Serial:       "XYZ999",
				SystemUUID:   uuid,
				OOBIP:        "10.10.4.55",
				PowerState:   "on",
				Claims: protocol.IdentityClaims{
					UUIDs:   []string{uuid},
					Serials: []string{"XYZ999"},
					MACs:    []string{"6c:3c:8c:aa:bb:01"},
				},
				Modules: []protocol.ModuleObservation{
					{Bay: "PSU 1", BayType: "power-supply", Profile: "Power supply", Manufacturer: "Dell", Model: "700W Titanium", Serial: "PSU001"},
				},
			}),
		},
		{
			ID: 2, CollectorID: "col-compute", Site: "hq", Source: "hostos", Profile: "local", Kind: "device", ObsKey: "srv-auto-55.local",
			ObservedAt: now,
			PayloadJSON: mustJSON(t, protocol.DeviceObservation{
				ObsKey:        "srv-auto-55.local",
				Site:          "hq",
				Name:          "srv-auto-55",
				RoleHint:      "server",
				Manufacturer:  "Dell",
				Model:         "PowerEdge R650",
				SystemUUID:    uuid,
				OSVersion:     "Ubuntu 24.04",
				KernelVersion: "6.8.0-generic",
				PrimaryIP:     "10.10.10.55",
				Claims: protocol.IdentityClaims{
					UUIDs: []string{uuid},
					MACs:  []string{"a4:bf:01:22:33:44"},
				},
				Interfaces: []protocol.InterfaceObservation{
					{
						Name:      "eno1",
						Type:      "1000base-t",
						SpeedMbps: 1000,
						MAC:       "a4:bf:01:22:33:44",
						IPAddresses: []protocol.IPObservation{
							{Address: "10.10.10.55/24", Primary: true},
						},
					},
				},
			}),
		},
	}

	res, err := Merge(inv, rawObs, nil)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	if len(res.State.Devices) != 1 {
		t.Fatalf("expected 1 merged auto device, got %d (%+v)", len(res.State.Devices), res.State.Devices)
	}
	dev := res.State.Devices[0]
	if dev.Origin != "auto" {
		t.Fatalf("expected origin=auto, got %q", dev.Origin)
	}
	if dev.OOBIP != "10.10.4.55" || dev.PrimaryIP != "10.10.10.55" {
		t.Fatalf("unexpected IPs: oob=%q primary=%q", dev.OOBIP, dev.PrimaryIP)
	}
	if len(dev.Sources) != 2 || len(dev.CollectorIDs) != 2 {
		t.Fatalf("expected 2 sources & 2 collectors, got sources=%v collectors=%v", dev.Sources, dev.CollectorIDs)
	}
	if len(dev.Modules) != 1 {
		t.Fatalf("expected 1 module from Redfish, got %d", len(dev.Modules))
	}
	if dev.ManualYAMLSnippet == "" {
		t.Fatalf("expected generated manual YAML snippet")
	}
	// Verify auto-generated device type with parametric faceplate exists.
	if len(res.State.DeviceTypes) != 1 || res.State.DeviceTypes[0].Faceplate == nil {
		t.Fatalf("expected synthesized device type with faceplate, got %+v", res.State.DeviceTypes)
	}

	// Re-merge with previous identities -> canonical ID must remain identical even if IP changes.
	rawObs[1].PayloadJSON = mustJSON(t, protocol.DeviceObservation{
		ObsKey:     "srv-auto-55-new",
		Site:       "hq",
		Name:       "srv-auto-55",
		SystemUUID: uuid,
		PrimaryIP:  "10.10.10.99",
		Claims:     protocol.IdentityClaims{UUIDs: []string{uuid}},
	})
	res2, err := Merge(inv, rawObs, res.Identities)
	if err != nil {
		t.Fatal(err)
	}
	if res2.State.Devices[0].ID != dev.ID {
		t.Fatalf("expected stable canonical ID %q, got %q", dev.ID, res2.State.Devices[0].ID)
	}
}

func TestIPReuseDoesNotMergeDifferentUUIDsAndManualWinsWithDrift(t *testing.T) {
	now := time.Now().UTC()
	vlan10 := 10
	inv := &manual.Inventory{
		Defaults: manual.Defaults{Site: "hq"},
		Rules:    manual.EffectiveRules{AutoServers: true, AutoNetworkDevices: true},
		Devices: []manual.DeviceSpec{
			{
				Name:   "srv-manual",
				Site:   "hq",
				Role:   "server",
				Serial: "MAN-111",
				Identity: &manual.IdentitySpec{
					SystemUUID: "11111111-1111-1111-1111-111111111111",
				},
				Interfaces: []manual.InterfaceSpec{
					{Name: "Port 1", UntaggedVLAN: &vlan10},
				},
			},
		},
		RackPlans: make(map[string]manual.RackPlan),
	}

	rawObs := []db.StoredObservation{
		// Same UUID as srv-manual, but different serial & VLAN -> manual wins + 2 drift warnings.
		{
			ID: 1, CollectorID: "c1", Site: "hq", Source: "redfish", Profile: "default", Kind: "device", ObsKey: "d1",
			ObservedAt: now,
			PayloadJSON: mustJSON(t, protocol.DeviceObservation{
				ObsKey:     "d1",
				Site:       "hq",
				SystemUUID: "11111111-1111-1111-1111-111111111111",
				Serial:     "LIVE-999",
				PrimaryIP:  "10.10.10.50",
				Claims:     protocol.IdentityClaims{UUIDs: []string{"11111111-1111-1111-1111-111111111111"}},
				Interfaces: []protocol.InterfaceObservation{{Name: "Port 1", UntaggedVLAN: 20}},
			}),
		},
		// Different UUID sharing the same IP 10.10.10.50 (e.g. DHCP lease reuse) -> MUST remain a distinct device!
		{
			ID: 2, CollectorID: "c1", Site: "hq", Source: "hostos", Profile: "default", Kind: "device", ObsKey: "d2",
			ObservedAt: now,
			PayloadJSON: mustJSON(t, protocol.DeviceObservation{
				ObsKey:     "d2",
				Site:       "hq",
				Name:       "srv-other",
				SystemUUID: "22222222-2222-2222-2222-222222222222",
				Serial:     "OTHER-222",
				PrimaryIP:  "10.10.10.50",
				Claims:     protocol.IdentityClaims{UUIDs: []string{"22222222-2222-2222-2222-222222222222"}},
			}),
		},
	}

	res, err := Merge(inv, rawObs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.State.Devices) != 2 {
		t.Fatalf("expected 2 separate devices due to disjoint UUID guardrail, got %d", len(res.State.Devices))
	}
	var manDevFound bool
	for _, d := range res.State.Devices {
		if d.Name == "srv-manual" {
			manDevFound = true
			if d.Serial != "MAN-111" {
				t.Fatalf("expected manual serial to win, got %q", d.Serial)
			}
			if d.Interfaces[0].UntaggedVLAN == nil || *d.Interfaces[0].UntaggedVLAN != 10 {
				t.Fatalf("expected manual VLAN 10 to win, got %v", d.Interfaces[0].UntaggedVLAN)
			}
		}
	}
	if !manDevFound {
		t.Fatalf("srv-manual missing")
	}
	if len(res.State.Issues) < 2 {
		t.Fatalf("expected serial and vlan drift warnings, got %+v", res.State.Issues)
	}
}

func TestManualConflictAndCableResolution(t *testing.T) {
	now := time.Now().UTC()
	inv := &manual.Inventory{
		Defaults: manual.Defaults{Site: "hq"},
		Rules:    manual.EffectiveRules{AutoNetworkDevices: true, AutoServers: true},
		Devices: []manual.DeviceSpec{
			{Name: "sw-a", Site: "hq", Role: "switch", Platform: "unifi", Serial: "SAME-SER", Interfaces: []manual.InterfaceSpec{{Name: "Port 1"}}},
			{Name: "sw-b", Site: "hq", Role: "switch", Serial: "SAME-SER", Interfaces: []manual.InterfaceSpec{{Name: "Port 2"}}},
		},
		RackPlans: make(map[string]manual.RackPlan),
	}

	rawObs := []db.StoredObservation{
		{
			ID: 1, CollectorID: "c1", Site: "hq", Source: "lldp", Profile: "default", Kind: "link", ObsKey: "l1",
			ObservedAt: now,
			PayloadJSON: mustJSON(t, protocol.LinkObservation{
				ObsKey: "l1", Site: "hq", Method: "lldp",
				LocalDeviceName: "sw-a", LocalPort: "eth0",
				RemoteDeviceName: "sw-b", RemotePort: "Port 2",
			}),
		},
		{
			ID: 2, CollectorID: "c1", Site: "hq", Source: "lldp", Profile: "default", Kind: "link", ObsKey: "l2",
			ObservedAt: now,
			PayloadJSON: mustJSON(t, protocol.LinkObservation{
				ObsKey: "l2", Site: "hq", Method: "lldp",
				LocalDeviceName: "sw-b", LocalPort: "Port 2",
				RemoteDeviceName: "sw-a", RemotePort: "eth0",
			}),
		},
	}

	res, err := Merge(inv, rawObs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.State.Devices) != 2 {
		t.Fatalf("manual devices must never merge together, got %d", len(res.State.Devices))
	}
	var conflictFound bool
	for _, iss := range res.State.Issues {
		if iss.Kind == "merge_conflict" {
			conflictFound = true
		}
	}
	if !conflictFound {
		t.Fatalf("expected merge_conflict issue, got %+v", res.State.Issues)
	}
	if len(res.State.Cables) != 1 {
		t.Fatalf("expected 1 deduplicated bidirectional cable, got %d (%+v)", len(res.State.Cables), res.State.Cables)
	}
	if res.State.Cables[0].Confidence != "verified" {
		t.Fatalf("expected bidirectional LLDP confidence=verified, got %q", res.State.Cables[0].Confidence)
	}
}
