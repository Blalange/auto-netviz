package sources_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/blalange/auto-netviz/internal/collector/sources/discovery"
	"github.com/blalange/auto-netviz/internal/collector/sources/hostos"
	"github.com/blalange/auto-netviz/internal/collector/sources/ipmi"
	"github.com/blalange/auto-netviz/internal/collector/sources/lldp"
	"github.com/blalange/auto-netviz/internal/collector/sources/redfish"
	"github.com/blalange/auto-netviz/internal/collector/sources/snmp"
	"github.com/blalange/auto-netviz/internal/collector/sources/unifi"
)

func TestUniFiBuildBatch(t *testing.T) {
	rawDevs := []map[string]any{
		{
			"mac": "24:5a:4c:76:79:d1", "name": "UDM Pro", "type": "udm", "model": "UDMPRO",
			"serial": "UDM123", "state": float64(1), "lan_ip": "192.168.50.1",
			"port_table": []any{
				map[string]any{"port_idx": float64(11), "media": "SFP+", "speed": float64(10000), "up": true},
			},
		},
		{
			"mac": "ac:8b:a9:b5:f7:e9", "name": "Core Switch", "type": "usw", "model": "US24PRO",
			"serial": "USW456", "state": float64(1), "ip": "192.168.50.178",
			"uplink": map[string]any{"uplink_mac": "24:5a:4c:76:79:d1", "port_idx": float64(25), "uplink_remote_port": float64(11), "speed": float64(10000)},
			"port_table": []any{
				map[string]any{"port_idx": float64(25), "media": "SFP+", "speed": float64(10000), "up": true, "port_poe": false},
			},
		},
	}
	rawNets := []map[string]any{
		{"name": "Default", "purpose": "corporate", "ip_subnet": "192.168.50.1/24"},
		{"name": "Servers", "vlan": float64(6), "purpose": "corporate", "ip_subnet": "192.168.6.1/24"},
	}

	batch, targets := unifi.BuildBatch("c1", "glemmen", "default", "glemmen", rawDevs, rawNets, nil, map[string]string{"US24PRO": "ubiquiti-usw-pro-24-poe"}, nil, true)
	if len(batch.Devices) != 2 || len(batch.Links) != 1 || len(batch.VLANs) != 2 || len(batch.Prefixes) != 2 {
		t.Fatalf("unexpected UniFi batch counts: devs=%d links=%d vlans=%d pfxs=%d", len(batch.Devices), len(batch.Links), len(batch.VLANs), len(batch.Prefixes))
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 active SNMP targets from UniFi, got %d", len(targets))
	}
}

func TestDecodeLLDPDU(t *testing.T) {
	var pld []byte
	addTLV := func(tlvType int, val []byte) {
		hdr := uint16((tlvType << 9) | (len(val) & 0x1ff))
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], hdr)
		pld = append(pld, b[:]...)
		pld = append(pld, val...)
	}
	addTLV(1, []byte{4, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}) // Chassis ID MAC
	addTLV(2, append([]byte{5}, []byte("Port 24")...))        // Port ID
	addTLV(3, []byte{0, 120})                                 // TTL 120
	addTLV(5, []byte("core-sw-01"))                           // SysName
	addTLV(7, []byte{0x00, 0x04, 0x00, 0x04})                 // Bridge enabled
	addTLV(8, []byte{5, 1, 10, 0, 0, 2})                      // Mgmt IP 10.0.0.2
	addTLV(0, nil)

	frame, err := lldp.DecodeLLDPDU(pld)
	if err != nil {
		t.Fatal(err)
	}
	if frame.ChassisID != "aa:bb:cc:dd:ee:ff" || frame.PortID != "Port 24" || frame.SysName != "core-sw-01" || frame.MgmtIP != "10.0.0.2" || frame.RoleHint != "switch" {
		t.Fatalf("unexpected decoded LLDP frame: %+v", frame)
	}
}

func TestRedfishMockServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/redfish/v1", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Systems":  map[string]string{"@odata.id": "/redfish/v1/Systems"},
			"Chassis":  map[string]string{"@odata.id": "/redfish/v1/Chassis"},
			"Managers": map[string]string{"@odata.id": "/redfish/v1/Managers"},
		})
	})
	mux.HandleFunc("/redfish/v1/Systems", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/1"}}})
	})
	mux.HandleFunc("/redfish/v1/Systems/1", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Manufacturer": "Dell Inc.", "Model": "PowerEdge R650", "SKU": "HL51JZ3",
			"UUID": "4c4c4544-0058-3910-804b-b7c04f324d31", "PowerState": "On", "HostName": "srv-01",
			"Processors": map[string]string{"@odata.id": "/redfish/v1/Systems/1/Processors"},
		})
	})
	mux.HandleFunc("/redfish/v1/Systems/1/Processors", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/1/Processors/CPU1"}}})
	})
	mux.HandleFunc("/redfish/v1/Systems/1/Processors/CPU1", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Socket": "CPU.Socket.1", "Manufacturer": "Intel", "Model": "Xeon", "TotalCores": 16})
	})

	ts := httptest.NewTLSServer(mux)
	defer ts.Close()

	obs, err := redfish.ReadRedfish(context.Background(), ts.URL, "10.0.0.10", "hq", "srv-01", "user", "pass", false, 5*time.Second, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if obs.Manufacturer != "Dell" || obs.ServiceTag != "HL51JZ3" || len(obs.Modules) != 1 {
		t.Fatalf("unexpected Redfish observation: %+v", obs)
	}
}

func TestIPMIParsers(t *testing.T) {
	fru := `FRU Device Description : Builtin FRU Device (ID 0)
 Product Manufacturer  : DELL
 Product Name          : PowerEdge R515
 Product Serial        : JVCJGZ1
FRU Device Description : PSU 1 (ID 1)
 Board Mfg             : DELL
 Board Product         : PWR SPL 750W
 Board Serial          : PSU9988`

	mfg, mdl, ser := ipmi.ParseFRU(fru)
	if mfg != "Dell" || mdl != "PowerEdge R515" || ser != "JVCJGZ1" {
		t.Fatalf("unexpected FRU: mfg=%q mdl=%q ser=%q", mfg, mdl, ser)
	}
	psus := ipmi.ParseFRUPSUs(fru, mfg)
	if len(psus) != 1 || psus[0].Bay != "PSU 1" {
		t.Fatalf("unexpected PSU FRUs: %+v", psus)
	}
	sdr := "Fan1A RPM | 4200 RPM | ok\nPS1 Status | Presence detected | ok"
	mods, sensors := ipmi.ParseSDR(sdr, mfg)
	if len(mods) != 2 || len(sensors) != 1 {
		t.Fatalf("unexpected SDR parse: mods=%+v sensors=%+v", mods, sensors)
	}
}

func TestHostOSParser(t *testing.T) {
	lscpu := `{"lscpu":[{"field":"Model name:","data":"AMD EPYC 7313P"},{"field":"CPU(s):","data":"16"}]}`
	lsblk := `{"blockdevices":[{"name":"nvme0n1","type":"disk","size":1000000000000,"model":"Samsung 980 PRO","serial":"S123","rota":false,"tran":"nvme"}]}`
	ipAddr := `[{"ifname":"eth0","operstate":"UP","mtu":1500,"address":"a4:bf:01:22:33:44","addr_info":[{"family":"inet","local":"10.10.10.80","prefixlen":24,"scope":"global"}]}]`

	obs := hostos.ParseHostProbeOutput("hq", "10.10.10.80", "worker-01", "Dell Inc.", "PowerEdge R630", "9SYD132", "4C4C4544-0053-5910-8044-B9C04F313332", `PRETTY_NAME="Ubuntu 24.04"`, "6.8.0", []byte(lscpu), []byte(lsblk), []byte(ipAddr))
	if obs.PrimaryIP != "10.10.10.80" || len(obs.Modules) != 2 || len(obs.Interfaces) != 1 {
		t.Fatalf("unexpected HostOS obs: %+v", obs)
	}
}

func TestUniFiGatewayMultiVLANAndClientOUI(t *testing.T) {
	rawDevs := []map[string]any{
		{"mac": "24:5a:4c:76:79:d1", "name": "UDM Pro", "type": "udm", "model": "UDMPRO", "serial": "UDM1", "state": float64(1), "lan_ip": "192.168.50.1"},
	}
	rawNets := []map[string]any{
		{"name": "Default", "purpose": "corporate", "ip_subnet": "192.168.50.1/24"},
		{"name": "OOB", "vlan": float64(4), "purpose": "corporate", "ip_subnet": "192.168.4.1/24"},
		{"name": "Servers", "vlan": float64(6), "purpose": "corporate", "ip_subnet": "192.168.6.1/24"},
	}
	rawClients := []map[string]any{
		{"mac": "08:9e:01:d8:7b:fb", "ip": "192.168.4.60", "oui": "Quanta Computer Inc.", "is_wired": true},
	}

	batch, _ := unifi.BuildBatch("c1", "glemmen", "default", "", rawDevs, rawNets, rawClients, map[string]string{"UDMPRO": "ubiquiti-udm-pro"}, nil, true)
	var lanIPs []string
	for _, d := range batch.Devices {
		if d.Name == "UDM Pro" {
			if d.ModelCode != "ubiquiti-udm-pro" {
				t.Fatalf("expected mapped ModelCode ubiquiti-udm-pro, got %q", d.ModelCode)
			}
			for _, iface := range d.Interfaces {
				if iface.Name == "lan" {
					for _, ip := range iface.IPAddresses {
						lanIPs = append(lanIPs, ip.Address)
					}
				}
			}
		}
		if d.ObsKey == "client:08:9e:01:d8:7b:fb" && len(d.Claims.Hostnames) != 0 {
			t.Fatalf("expected OUI vendor string not to enter Claims.Hostnames, got %v", d.Claims.Hostnames)
		}
	}
	joined := strings.Join(lanIPs, ",")
	for _, want := range []string{"192.168.50.1/24", "192.168.4.1/24", "192.168.6.1/24"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected gateway lan interface to contain %s, got %s", want, joined)
		}
	}
}

func TestRedfishGenericHostnameFiltered(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/redfish/v1", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Systems": map[string]string{"@odata.id": "/redfish/v1/Systems"}})
	})
	mux.HandleFunc("/redfish/v1/Systems", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/1"}}})
	})
	mux.HandleFunc("/redfish/v1/Systems/1", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Manufacturer": "Dell Inc.", "Model": "PowerEdge R250", "SKU": "XYZ", "HostName": "dellinc"})
	})
	ts := httptest.NewTLSServer(mux)
	defer ts.Close()

	obs, err := redfish.ReadRedfish(context.Background(), ts.URL, "192.168.4.80", "glemmen", "", "u", "p", false, 5*time.Second, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Claims.Hostnames) != 0 {
		t.Fatalf("expected generic HostName dellinc stripped, got %v", obs.Claims.Hostnames)
	}
}

func TestDiscoveryProbeRedfishAndSNMPQBridgeMAC(t *testing.T) {
	nonRedfish := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>Router UI</body></html>"))
	}))
	defer nonRedfish.Close()
	if discovery.ProbeRedfishURL(context.Background(), nonRedfish.URL, 2*time.Second) {
		t.Fatalf("expected non-Redfish HTML 200 response to return false")
	}

	realRedfish := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"RedfishVersion":"1.6.0","@odata.id":"/redfish/v1"}`))
	}))
	defer realRedfish.Close()
	if !discovery.ProbeRedfishURL(context.Background(), realRedfish.URL, 2*time.Second) {
		t.Fatalf("expected valid Redfish ServiceRoot to return true")
	}

	unauthNonOdata := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer unauthNonOdata.Close()
	if discovery.ProbeRedfishURL(context.Background(), unauthNonOdata.URL, 2*time.Second) {
		t.Fatalf("expected 401 without OData-Version header to return false")
	}

	if got := snmp.ParseQBridgeMAC(".1.3.6.1.2.1.17.7.1.2.2.1.2.4.8.158.1.216.123.251"); got != "08:9e:01:d8:7b:fb" {
		t.Fatalf("expected 08:9e:01:d8:7b:fb, got %q", got)
	}
}
