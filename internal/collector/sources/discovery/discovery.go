package discovery

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/blalange/auto-netviz/internal/collector/config"
	"github.com/blalange/auto-netviz/internal/collector/sources"
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/protocol"
)

type Source struct {
	Profiles []config.DiscoveryProfile
}

func (s *Source) Name() string { return "discovery" }

func (s *Source) Enabled() bool {
	for _, p := range s.Profiles {
		if config.IsEnabled(p.Enabled) {
			return true
		}
	}
	return false
}

func (s *Source) Collect(ctx context.Context, rt sources.Runtime) ([]protocol.ObservationBatch, []protocol.SourceHealthPayload, error) {
	var batches []protocol.ObservationBatch
	var healths []protocol.SourceHealthPayload
	var allProbed []sources.ProbedEndpoint

	for _, p := range s.Profiles {
		if !config.IsEnabled(p.Enabled) {
			continue
		}
		start := time.Now()
		site := p.Site
		if site == "" {
			site = rt.Site
		}
		h := protocol.SourceHealthPayload{Source: "discovery", Profile: p.Name, Enabled: true, LastRunAt: &start}

		timeout := config.ParseDuration(p.Timeout, 1500*time.Millisecond)
		concurrency := p.Concurrency
		if concurrency <= 0 {
			concurrency = 32
		}

		// Map IP -> discovered info
		type hostRecord struct {
			ip         string
			mac        string
			hostname   string
			openPorts  []int
			hasRedfish bool
			hasSNMP    bool
			hasIPMI    bool
			hasSSH     bool
		}
		records := make(map[string]*hostRecord)
		ensure := func(ip string) *hostRecord {
			ip = identity.NormalizeIP(ip)
			if ip == "" {
				return nil
			}
			if r, ok := records[ip]; ok {
				return r
			}
			r := &hostRecord{ip: ip}
			records[ip] = r
			return r
		}

		// 1. Local ARP / Neighbor table
		if config.IsEnabled(p.ReadLocalARP) {
			for _, arp := range readARPNeighbors(ctx) {
				if r := ensure(arp.IP); r != nil {
					if r.mac == "" {
						r.mac = arp.MAC
					}
				}
			}
		}

		// 2. DHCP leases (dnsmasq style: <ts> <mac> <ip> <hostname> ...)
		for _, leaseFile := range p.DHCPLeaseFiles {
			for _, l := range parseDHCPLeases(leaseFile) {
				if r := ensure(l.IP); r != nil {
					if r.mac == "" {
						r.mac = l.MAC
					}
					if r.hostname == "" {
						r.hostname = l.Hostname
					}
				}
			}
		}

		// 3. Expand target CIDRs (capped at 1024 hosts per profile for safety)
		exclude := make(map[netip.Prefix]bool)
		for _, ex := range p.Exclude {
			if pfx, err := netip.ParsePrefix(ex); err == nil {
				exclude[pfx] = true
			} else if addr, err := netip.ParseAddr(ex); err == nil {
				exclude[netip.PrefixFrom(addr, addr.BitLen())] = true
			}
		}

		var probeTargets []string
		for ip := range records {
			probeTargets = append(probeTargets, ip)
		}
		for _, cidr := range p.CIDRs {
			ips := expandCIDR(cidr, 1024)
			for _, ip := range ips {
				addr, err := netip.ParseAddr(ip)
				if err != nil {
					continue
				}
				skip := false
				for exPfx := range exclude {
					if exPfx.Contains(addr) {
						skip = true
						break
					}
				}
				if !skip {
					probeTargets = append(probeTargets, ip)
				}
			}
		}

		probeTargets = uniqueStrings(probeTargets)
		ports := p.ProbePorts
		if len(ports) == 0 {
			ports = []int{22, 80, 443}
		}

		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, concurrency)

		for _, ip := range probeTargets {
			wg.Add(1)
			go func(targetIP string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				var open []int
				for _, port := range ports {
					if tcpReachable(ctx, targetIP, port, timeout) {
						open = append(open, port)
					}
				}
				redfishOK := false
				if config.IsEnabled(p.ProbeRedfish) && (containsInt(open, 443) || len(open) == 0) {
					redfishOK = probeRedfishEndpoint(ctx, targetIP, timeout)
				}
				snmpOK := false
				if config.IsEnabled(p.ProbeSNMP) {
					// Light SNMP UDP probe if SNMP port 161 listed or probed
					snmpOK = containsInt(open, 161)
				}

				if len(open) == 0 && !redfishOK {
					// If only known via ARP, keep it; otherwise drop unreachable IP from CIDR scan.
					mu.Lock()
					_, hasARP := records[targetIP]
					mu.Unlock()
					if !hasARP {
						return
					}
				}

				ptrName := ""
				if config.IsEnabled(p.ReverseDNS) {
					ptrName = reverseLookup(ctx, targetIP, timeout)
				}

				mu.Lock()
				r := ensure(targetIP)
				r.openPorts = open
				r.hasRedfish = redfishOK
				r.hasSNMP = snmpOK
				r.hasSSH = containsInt(open, 22)
				r.hasIPMI = containsInt(open, 623)
				if r.hostname == "" && ptrName != "" {
					r.hostname = ptrName
				}
				mu.Unlock()
			}(ip)
		}
		wg.Wait()

		var devs []protocol.DeviceObservation
		for _, r := range records {
			mfg := ""
			if r.mac != "" {
				mfg = identity.LookupVendor(r.mac)
			}
			role := inferRoleFromPorts(r.openPorts, r.hasRedfish, r.hasSNMP, mfg)
			claims := protocol.IdentityClaims{
				Locators: map[string]string{"mgmt_ip": r.ip},
			}
			if r.mac != "" && !identity.IsInvalidMAC(r.mac) && !identity.IsLocallyAdministeredMAC(r.mac) {
				claims.MACs = []string{r.mac}
			}
			if r.hostname != "" {
				claims.Hostnames = []string{r.hostname}
			}
			if r.hasRedfish {
				claims.Locators["redfish_ip"] = r.ip
			}
			if r.hasSNMP {
				claims.Locators["snmp_ip"] = r.ip
			}

			var ifaces []protocol.InterfaceObservation
			ifaces = append(ifaces, protocol.InterfaceObservation{
				Name:        "eth0",
				Type:        "1000base-t",
				Kind:        "physical",
				MAC:         r.mac,
				IPAddresses: []protocol.IPObservation{{Address: r.ip + "/24", DNSName: r.hostname, Primary: true}},
			})

			devs = append(devs, protocol.DeviceObservation{
				ObsKey:       firstNonEmpty(r.mac, r.ip),
				Site:         site,
				Name:         r.hostname,
				RoleHint:     role,
				Manufacturer: mfg,
				Status:       "active",
				PrimaryIP:    r.ip,
				Claims:       claims,
				Interfaces:   ifaces,
				Attributes:   map[string]any{"open_ports": r.openPorts},
			})

			if r.hasRedfish || r.hasSNMP || r.hasIPMI || r.hasSSH {
				allProbed = append(allProbed, sources.ProbedEndpoint{
					Site:       site,
					IP:         r.ip,
					HasSNMP:    r.hasSNMP,
					HasRedfish: r.hasRedfish,
					HasIPMI:    r.hasIPMI,
					HasSSH:     r.hasSSH,
				})
			}
		}

		batches = append(batches, protocol.ObservationBatch{
			BatchID:      fmt.Sprintf("discovery-%s-%d", p.Name, time.Now().UnixNano()),
			CollectorID:  rt.CollectorID,
			Site:         site,
			Source:       "discovery",
			Profile:      p.Name,
			CollectedAt:  time.Now().UTC(),
			TTLSeconds:   600,
			FullSnapshot: true,
			Devices:      devs,
		})

		done := time.Now()
		h.Status = "ok"
		h.LastSuccessAt = &done
		h.DurationMs = time.Since(start).Milliseconds()
		h.DeviceCount = len(devs)
		healths = append(healths, h)
	}

	if rt.Shared != nil {
		rt.Shared.SetProbed(allProbed)
	}
	return batches, healths, nil
}

type arpEntry struct {
	IP  string
	MAC string
}

func readARPNeighbors(ctx context.Context) []arpEntry {
	var out []arpEntry
	// Try Linux /proc/net/arp first (no external binary required)
	if raw, err := os.ReadFile("/proc/net/arp"); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(raw)))
		first := true
		for sc.Scan() {
			if first {
				first = false
				continue
			}
			fields := strings.Fields(sc.Text())
			if len(fields) >= 4 {
				mac, err := identity.NormalizeMAC(fields[3])
				if err == nil && !identity.IsInvalidMAC(mac) {
					out = append(out, arpEntry{IP: fields[0], MAC: mac})
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	// Fallback: `ip -j neigh`
	if bin, err := exec.LookPath("ip"); err == nil {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		b, err := exec.CommandContext(cctx, bin, "-j", "neigh", "show").Output()
		cancel()
		if err == nil {
			var rows []struct {
				Dst    string `json:"dst"`
				LLAddr string `json:"lladdr"`
			}
			if json.Unmarshal(b, &rows) == nil {
				for _, r := range rows {
					if mac, err := identity.NormalizeMAC(r.LLAddr); err == nil && !identity.IsInvalidMAC(mac) {
						out = append(out, arpEntry{IP: r.Dst, MAC: mac})
					}
				}
			}
		}
	}
	return out
}

type dhcpLease struct {
	IP, MAC, Hostname string
}

func parseDHCPLeases(path string) []dhcpLease {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []dhcpLease
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		// dnsmasq format: <expiry> <mac> <ip> <hostname> <clientid>
		if len(fields) >= 4 {
			if mac, err := identity.NormalizeMAC(fields[1]); err == nil {
				host := fields[3]
				if host == "*" {
					host = ""
				}
				out = append(out, dhcpLease{MAC: mac, IP: fields[2], Hostname: host})
			}
		}
	}
	return out
}

func tcpReachable(ctx context.Context, ip string, port int, timeout time.Duration) bool {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, fmt.Sprintf("%d", port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func probeRedfishEndpoint(ctx context.Context, ip string, timeout time.Duration) bool {
	client := &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+ip+"/redfish/v1", nil)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized
}

func reverseLookup(ctx context.Context, ip string, timeout time.Duration) string {
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(rctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

func expandCIDR(cidr string, maxHosts int) []string {
	pfx, err := netip.ParsePrefix(cidr)
	if err != nil || !pfx.Addr().Is4() {
		return nil
	}
	var out []string
	for addr := pfx.Masked().Addr().Next(); pfx.Contains(addr); addr = addr.Next() {
		if !addr.Next().IsValid() || !pfx.Contains(addr.Next()) && pfx.Bits() < 31 {
			break // skip broadcast
		}
		out = append(out, addr.String())
		if len(out) >= maxHosts {
			break
		}
	}
	return out
}

func inferRoleFromPorts(open []int, hasRedfish, hasSNMP bool, mfg string) string {
	if hasRedfish {
		return "server"
	}
	if hasSNMP {
		return "switch"
	}
	if mfg == "APC" {
		return "power"
	}
	if containsInt(open, 8006) {
		return "server" // Proxmox VE
	}
	if containsInt(open, 22) {
		return "server"
	}
	return "unknown"
}

func containsInt(list []int, target int) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
