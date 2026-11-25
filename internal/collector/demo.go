package collector

import (
	"fmt"
	"time"

	"github.com/blalange/auto-netviz/internal/protocol"
)

// BuildDemoBatches generates realistic multi-source collector observations matching
// examples/manual plus auto-discovered devices and live hardware/VLAN telemetry.
func BuildDemoBatches(collectorID, site string) []protocol.ObservationBatch {
	if site == "" {
		site = "hq"
	}
	now := time.Now().UTC()
	up := true
	down := false

	// 1. UniFi / SNMP switch telemetry for hq-gw-01 and hq-sw-01 + an auto-discovered edge AP!
	unifiBatch := protocol.ObservationBatch{
		BatchID:      fmt.Sprintf("demo-unifi-%d", now.Unix()),
		CollectorID:  collectorID,
		Site:         site,
		Source:       "unifi",
		Profile:      "demo-unifi",
		CollectedAt:  now,
		TTLSeconds:   600,
		FullSnapshot: true,
		Devices: []protocol.DeviceObservation{
			{
				ObsKey: "24:5a:4c:10:00:01", Site: site, Name: "hq-gw-01",
				RoleHint: "gateway", Manufacturer: "Ubiquiti", Model: "Dream Machine Pro",
				Platform: "unifi", Status: "active", Serial: "UDMP77889900",
				FirmwareVersion: "4.0.21", PrimaryIP: "10.10.10.1",
				Claims: protocol.IdentityClaims{
					Serials: []string{"UDMP77889900"},
					MACs:    []string{"24:5a:4c:10:00:01"},
					Locators: map[string]string{"unifi_ip": "10.10.10.1"},
				},
				Interfaces: []protocol.InterfaceObservation{
					{Name: "Port 1", Type: "1000base-t", Kind: "physical", Enabled: &up, LinkUp: &down, SpeedMbps: 0},
					{Name: "Port 9", Type: "2.5gbase-t", Kind: "physical", Enabled: &up, LinkUp: &up, SpeedMbps: 2500, Description: "WAN Fiber", IPAddresses: []protocol.IPObservation{{Address: "198.51.100.14/24", Description: "WAN"}}},
					{Name: "Port 11", Type: "10gbase-x-sfpp", Kind: "physical", Enabled: &up, LinkUp: &up, SpeedMbps: 10000, Description: "Downlink to hq-sw-01"},
				},
			},
			{
				ObsKey: "ac:8b:a9:20:00:01", Site: site, Name: "hq-sw-01",
				RoleHint: "switch", Manufacturer: "Ubiquiti", Model: "USW-Pro-24-PoE",
				Platform: "unifi", Status: "active", Serial: "USW2411223344",
				FirmwareVersion: "7.1.26", PrimaryIP: "10.10.10.2",
				Claims: protocol.IdentityClaims{
					Serials: []string{"USW2411223344"},
					MACs:    []string{"ac:8b:a9:20:00:01"},
					Locators: map[string]string{"unifi_ip": "10.10.10.2", "snmp_ip": "10.10.10.2"},
				},
				Interfaces: []protocol.InterfaceObservation{
					{Name: "Port 1", Type: "1000base-t", Kind: "physical", Enabled: &up, LinkUp: &up, SpeedMbps: 1000, Mode: "access", UntaggedVLAN: 10, Description: "node-01 eno1"},
					{Name: "Port 2", Type: "1000base-t", Kind: "physical", Enabled: &up, LinkUp: &up, SpeedMbps: 1000, Mode: "access", UntaggedVLAN: 10, Description: "node-02 eno1"},
					{Name: "Port 3", Type: "10gbase-t", Kind: "physical", Enabled: &up, LinkUp: &up, SpeedMbps: 10000, Mode: "tagged", UntaggedVLAN: 10, TaggedVLANs: []int{20}, Description: "storage-01 ens1f0"},
					{Name: "Port 4", Type: "1000base-t", Kind: "physical", Enabled: &up, LinkUp: &up, SpeedMbps: 1000, PoEMode: "pse", PoEWatts: 11.4, Mode: "tagged", UntaggedVLAN: 1, TaggedVLANs: []int{10}, Description: "hq-ap-01"},
					{Name: "Port 25", Type: "10gbase-x-sfpp", Kind: "physical", Enabled: &up, LinkUp: &up, SpeedMbps: 10000, Mode: "tagged", UntaggedVLAN: 1, TaggedVLANs: []int{4, 10, 20}, Description: "Uplink to hq-gw-01"},
					{Name: "Port 26", Type: "10gbase-x-sfpp", Kind: "physical", Enabled: &up, LinkUp: &up, SpeedMbps: 10000, Mode: "tagged", UntaggedVLAN: 10, TaggedVLANs: []int{20}, Description: "node-01 ens1f0"},
				},
			},
			// Auto-discovered ceiling AP (not in manual YAML!)
			{
				ObsKey: "74:ac:b9:30:00:05", Site: site, Name: "hq-ap-01",
				RoleHint: "access-point", Manufacturer: "Ubiquiti", Model: "U6-Pro",
				Platform: "unifi", Status: "active", Serial: "UAP6PRO9911",
				FirmwareVersion: "6.6.77", PrimaryIP: "10.10.10.45",
				Claims: protocol.IdentityClaims{
					Serials: []string{"UAP6PRO9911"},
					MACs:    []string{"74:ac:b9:30:00:05"},
					Locators: map[string]string{"unifi_ip": "10.10.10.45"},
				},
				Interfaces: []protocol.InterfaceObservation{
					{Name: "Port 1", Type: "1000base-t", Kind: "physical", Enabled: &up, LinkUp: &up, SpeedMbps: 1000, MAC: "74:ac:b9:30:00:05", IPAddresses: []protocol.IPObservation{{Address: "10.10.10.45/24", DNSName: "hq-ap-01", Primary: true}}},
					{Name: "wifi5g", Type: "ieee802.11ax", Kind: "wireless", Description: "5 GHz Wi-Fi 6"},
				},
			},
		},
		Links: []protocol.LinkObservation{
			{
				ObsKey: "unifi:ap01->sw01", Site: site, Method: "unifi_uplink",
				LocalObsKey: "74:ac:b9:30:00:05", LocalDeviceName: "hq-ap-01", LocalPort: "Port 1",
				RemoteObsKey: "ac:8b:a9:20:00:01", RemoteDeviceName: "hq-sw-01", RemotePort: "Port 4",
				CableType: "cat6", SpeedMbps: 1000, Description: "PoE+ uplink to hq-sw-01 Port 4",
			},
		},
	}

	// 2. Redfish + Kubernetes hardware telemetry for node-01 & node-02 + IPMI for storage-01
	redfishBatch := protocol.ObservationBatch{
		BatchID:      fmt.Sprintf("demo-redfish-%d", now.Unix()),
		CollectorID:  collectorID,
		Site:         site,
		Source:       "redfish",
		Profile:      "demo-idrac",
		CollectedAt:  now,
		TTLSeconds:   900,
		FullSnapshot: true,
		Devices: []protocol.DeviceObservation{
			{
				ObsKey: "10.10.4.11", Site: site, Name: "node-01", RoleHint: "control-plane",
				Manufacturer: "Dell", Model: "PowerEdge R650", Serial: "7X9K2M1", ServiceTag: "7X9K2M1",
				SystemUUID: "4c4c4544-0058-3910-804b-b7c04f324d31",
				PowerState: "on", FirmwareVersion: "1.14.2", OOBIP: "10.10.4.11", Status: "active",
				Claims: protocol.IdentityClaims{
					UUIDs:   []string{"4c4c4544-0058-3910-804b-b7c04f324d31"},
					Serials: []string{"7X9K2M1"},
					Locators: map[string]string{"redfish_ip": "10.10.4.11"},
				},
				Modules: []protocol.ModuleObservation{
					{Bay: "CPU 1", BayType: "cpu-socket", Profile: "CPU", Manufacturer: "Intel", Model: "Xeon Silver 4314", Status: "active"},
					{Bay: "DIMM A1", BayType: "dimm-ddr4", Profile: "Memory", Manufacturer: "Micron", Model: "32GB DDR4-3200", Serial: "MEM-01", Status: "active"},
					{Bay: "Drive 1", BayType: "drive-2-5", Profile: "Hard disk", Manufacturer: "Samsung", Model: "PM9A3 1.92TB NVMe", Serial: "NVME-101", Status: "active"},
					{Bay: "Drive 2", BayType: "drive-2-5", Profile: "Hard disk", Manufacturer: "Samsung", Model: "PM9A3 1.92TB NVMe", Serial: "NVME-102", Status: "active"},
					{Bay: "PSU 1", BayType: "power-supply", Profile: "Power supply", Manufacturer: "Dell", Model: "800W Platinum", Serial: "PSU-11A", Status: "active"},
					{Bay: "PSU 2", BayType: "power-supply", Profile: "Power supply", Manufacturer: "Dell", Model: "800W Platinum", Serial: "PSU-11B", Status: "active"},
				},
			},
			{
				ObsKey: "10.10.4.12", Site: site, Name: "node-02", RoleHint: "worker",
				Manufacturer: "Dell", Model: "PowerEdge R650", Serial: "7X9K2M2", ServiceTag: "7X9K2M2",
				SystemUUID: "4c4c4544-0058-3910-804b-b7c04f324d32",
				PowerState: "on", FirmwareVersion: "1.14.2", OOBIP: "10.10.4.12", Status: "active",
				Claims: protocol.IdentityClaims{
					UUIDs:   []string{"4c4c4544-0058-3910-804b-b7c04f324d32"},
					Serials: []string{"7X9K2M2"},
					Locators: map[string]string{"redfish_ip": "10.10.4.12"},
				},
				Modules: []protocol.ModuleObservation{
					{Bay: "CPU 1", BayType: "cpu-socket", Profile: "CPU", Manufacturer: "Intel", Model: "Xeon Silver 4314", Status: "active"},
					{Bay: "Drive 1", BayType: "drive-2-5", Profile: "Hard disk", Manufacturer: "Samsung", Model: "PM9A3 1.92TB NVMe", Serial: "NVME-201", Status: "active"},
					{Bay: "PSU 1", BayType: "power-supply", Profile: "Power supply", Manufacturer: "Dell", Model: "800W Platinum", Serial: "PSU-12A", Status: "active"},
				},
			},
		},
	}

	k8sBatch := protocol.ObservationBatch{
		BatchID:      fmt.Sprintf("demo-k8s-%d", now.Unix()),
		CollectorID:  collectorID,
		Site:         site,
		Source:       "kubernetes",
		Profile:      "hq-k8s",
		CollectedAt:  now,
		TTLSeconds:   300,
		FullSnapshot: true,
		Devices: []protocol.DeviceObservation{
			{
				ObsKey: "hq-k8s/node-01", Site: site, Cluster: "hq-k8s", Name: "node-01",
				RoleHint: "control-plane", Platform: "talos-linux", Status: "active",
				SystemUUID: "4c4c4544-0058-3910-804b-b7c04f324d31",
				OSVersion: "Talos (v1.14.5)", KernelVersion: "6.18.58-talos", PrimaryIP: "10.10.10.11",
				Claims: protocol.IdentityClaims{
					UUIDs:    []string{"4c4c4544-0058-3910-804b-b7c04f324d31"},
					Locators: map[string]string{"k8s_node": "node-01", "cluster": "hq-k8s"},
				},
				Interfaces: []protocol.InterfaceObservation{
					{Name: "eno1", Type: "1000base-t", Kind: "physical", LinkUp: &up, SpeedMbps: 1000, MAC: "6c:3c:8c:10:00:12", IPAddresses: []protocol.IPObservation{{Address: "10.10.10.11/24", DNSName: "node-01", Primary: true}}},
					{Name: "ens1f0", Type: "10gbase-x-sfpp", Kind: "physical", LinkUp: &up, SpeedMbps: 10000, MAC: "a4:bf:01:10:00:11"},
				},
			},
			{
				ObsKey: "hq-k8s/node-02", Site: site, Cluster: "hq-k8s", Name: "node-02",
				RoleHint: "worker", Platform: "talos-linux", Status: "active",
				SystemUUID: "4c4c4544-0058-3910-804b-b7c04f324d32",
				OSVersion: "Talos (v1.14.5)", KernelVersion: "6.18.58-talos", PrimaryIP: "10.10.10.12",
				Claims: protocol.IdentityClaims{
					UUIDs:    []string{"4c4c4544-0058-3910-804b-b7c04f324d32"},
					Locators: map[string]string{"k8s_node": "node-02", "cluster": "hq-k8s"},
				},
				Interfaces: []protocol.InterfaceObservation{
					{Name: "eno1", Type: "1000base-t", Kind: "physical", LinkUp: &up, SpeedMbps: 1000, MAC: "6c:3c:8c:10:00:22", IPAddresses: []protocol.IPObservation{{Address: "10.10.10.12/24", DNSName: "node-02", Primary: true}}},
				},
			},
		},
	}

	// 3. Discovery auto-creates an unmanaged Synology NAS
	discBatch := protocol.ObservationBatch{
		BatchID:      fmt.Sprintf("demo-discovery-%d", now.Unix()),
		CollectorID:  collectorID,
		Site:         site,
		Source:       "discovery",
		Profile:      "hq-lan",
		CollectedAt:  now,
		TTLSeconds:   600,
		FullSnapshot: true,
		Devices: []protocol.DeviceObservation{
			{
				ObsKey: "00:11:32:aa:bb:cc", Site: site, Name: "nas-backup-01",
				RoleHint: "storage", Manufacturer: "Synology", Model: "RS1221+",
				Status: "active", PrimaryIP: "10.10.20.40",
				Claims: protocol.IdentityClaims{
					MACs:      []string{"00:11:32:aa:bb:cc"},
					Hostnames: []string{"nas-backup-01"},
					Locators:  map[string]string{"mgmt_ip": "10.10.20.40"},
				},
				Interfaces: []protocol.InterfaceObservation{
					{Name: "eth0", Type: "1000base-t", Kind: "physical", LinkUp: &up, SpeedMbps: 1000, MAC: "00:11:32:aa:bb:cc", IPAddresses: []protocol.IPObservation{{Address: "10.10.20.40/24", DNSName: "nas-backup-01", Primary: true}}},
				},
			},
		},
	}

	return []protocol.ObservationBatch{unifiBatch, redfishBatch, k8sBatch, discBatch}
}

func DemoHealth() []protocol.SourceHealthPayload {
	now := time.Now().UTC()
	srcs := []string{"unifi", "snmp", "lldp", "redfish", "ipmi", "discovery", "hostos", "kubernetes"}
	out := make([]protocol.SourceHealthPayload, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, protocol.SourceHealthPayload{
			Source: s, Profile: "demo", Enabled: true, Status: "ok",
			LastRunAt: &now, LastSuccessAt: &now, DurationMs: 42,
		})
	}
	return out
}
