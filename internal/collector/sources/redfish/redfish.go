package redfish

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/blalange/auto-netviz/internal/collector/config"
	"github.com/blalange/auto-netviz/internal/collector/sources"
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/protocol"
)

type Source struct {
	Profiles []config.RedfishProfile
}

func (s *Source) Name() string { return "redfish" }

func (s *Source) Enabled() bool {
	for _, p := range s.Profiles {
		if config.IsEnabled(p.Enabled) {
			return true
		}
	}
	return false
}

type target struct {
	ip, name, site, role string
	tags                 []string
	profile              config.RedfishProfile
}

func (s *Source) Collect(ctx context.Context, rt sources.Runtime) ([]protocol.ObservationBatch, []protocol.SourceHealthPayload, error) {
	seen := make(map[string]bool)
	var jobs []target

	add := func(ip, name, site, role string, tags []string, explicit string, override *config.RedfishProfile) {
		ip = identity.NormalizeIP(ip)
		if ip == "" {
			return
		}
		if site == "" {
			site = rt.Site
		}
		key := site + "|" + ip
		if seen[key] {
			return
		}
		var prof *config.RedfishProfile
		var err error
		if override != nil {
			prof = override
		} else {
			prof, err = config.MatchRedfishProfile(s.Profiles, explicit, site, name, role, tags, ip)
			if err != nil {
				return
			}
		}
		seen[key] = true
		jobs = append(jobs, target{ip: ip, name: name, site: site, role: role, tags: tags, profile: *prof})
	}

	for _, p := range s.Profiles {
		if !config.IsEnabled(p.Enabled) {
			continue
		}
		for _, t := range p.Targets {
			pc := p
			add(t.IP, t.Name, t.Site, t.Role, nil, p.Name, &pc)
		}
	}
	for _, h := range rt.Hints {
		if h.RedfishIP != "" {
			add(h.RedfishIP, h.DeviceName, h.Site, h.Role, h.Tags, h.RedfishCredential, nil)
		}
	}
	if rt.Shared != nil {
		for _, pr := range rt.Shared.Probed() {
			if pr.HasRedfish {
				add(pr.IP, "", pr.Site, "server", nil, "", nil)
			}
		}
	}

	if len(jobs) == 0 {
		return nil, []protocol.SourceHealthPayload{{
			Source: "redfish", Profile: "default", Enabled: true, Status: "ok",
		}}, nil
	}

	byProf := make(map[string][]target)
	for _, j := range jobs {
		byProf[j.profile.Name] = append(byProf[j.profile.Name], j)
	}

	var batches []protocol.ObservationBatch
	var healths []protocol.SourceHealthPayload

	for profName, pJobs := range byProf {
		start := time.Now()
		prof := pJobs[0].profile
		h := protocol.SourceHealthPayload{Source: "redfish", Profile: profName, Enabled: true, LastRunAt: &start}
		user, errU := prof.Username.Resolve(ctx, rt.BaseDir, rt.K8s, "")
		pass, errP := prof.Password.Resolve(ctx, rt.BaseDir, rt.K8s, "")
		if errU != nil || errP != nil || user == "" || pass == "" {
			h.Status = "error"
			h.Error = "missing Redfish credentials"
			h.DurationMs = time.Since(start).Milliseconds()
			healths = append(healths, h)
			continue
		}

		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 6)
		var devs []protocol.DeviceObservation
		var errs []string

		for _, job := range pJobs {
			wg.Add(1)
			go func(j target) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				obs, err := ReadRedfish(ctx, "https://"+j.ip, j.ip, j.site, j.name, user, pass, prof.VerifyTLS, config.ParseDuration(prof.Timeout, 15*time.Second), prof.SystemPath, prof.ChassisPath)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: %v", j.ip, err))
					return
				}
				devs = append(devs, obs)
			}(job)
		}
		wg.Wait()

		batches = append(batches, protocol.ObservationBatch{
			BatchID:      fmt.Sprintf("redfish-%s-%d", profName, time.Now().UnixNano()),
			CollectorID:  rt.CollectorID,
			Site:         rt.Site,
			Source:       "redfish",
			Profile:      profName,
			CollectedAt:  time.Now().UTC(),
			TTLSeconds:   900,
			FullSnapshot: true,
			Devices:      devs,
			Errors:       errs,
		})

		done := time.Now()
		h.LastSuccessAt = &done
		h.DurationMs = time.Since(start).Milliseconds()
		h.DeviceCount = len(devs)
		if len(errs) > 0 && len(devs) == 0 {
			h.Status = "error"
			h.Error = errs[0]
		} else {
			h.Status = "ok"
			if len(errs) > 0 {
				h.Error = fmt.Sprintf("%d partial error(s)", len(errs))
			}
		}
		healths = append(healths, h)
	}
	return batches, healths, nil
}

// ReadRedfish queries DMTF Redfish endpoints on baseURL and builds a normalized DeviceObservation.
func ReadRedfish(
	ctx context.Context,
	baseURL, ip, site, hintName, username, password string,
	verifyTLS bool,
	timeout time.Duration,
	systemPathOverride, chassisPathOverride string,
) (protocol.DeviceObservation, error) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS}, //nolint:gosec
		},
	}

	get := func(path string) (map[string]any, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
		req.SetBasicAuth(username, password)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d on %s", resp.StatusCode, path)
		}
		var m map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
			return nil, err
		}
		return m, nil
	}

	root, _ := get("/redfish/v1")
	discoverFirst := func(collectionKey, fallback string) string {
		colURL := odataID(root[collectionKey])
		if colURL == "" {
			colURL = "/redfish/v1/" + collectionKey
		}
		if col, err := get(colURL); err == nil {
			if members, ok := col["Members"].([]any); ok {
				for _, m := range members {
					if mm, ok := m.(map[string]any); ok {
						if p := odataID(mm); p != "" {
							return p
						}
					}
				}
			}
		}
		return fallback
	}

	sysURL := systemPathOverride
	if sysURL == "" {
		sysURL = discoverFirst("Systems", "/redfish/v1/Systems/System.Embedded.1")
	}
	sys, err := get(sysURL)
	if err != nil {
		return protocol.DeviceObservation{}, err
	}

	mfg := normalizeVendor(str(sys["Manufacturer"]))
	mdl := strings.TrimSpace(str(sys["Model"]))
	serial := identity.NormalizeSerial(firstNonEmpty(str(sys["SKU"]), str(sys["SerialNumber"])))
	sysUUID := identity.NormalizeUUID(str(sys["UUID"]))
	powerState := strings.ToLower(str(sys["PowerState"]))
	biosVer := str(sys["BiosVersion"])
	hostName := str(sys["HostName"])
	if isGenericRedfishHostname(hostName) {
		hostName = ""
	}
	if hostName == "" {
		hostName = hintName
	}

	var modules []protocol.ModuleObservation
	var sensors []protocol.SensorObservation
	var ifaces []protocol.InterfaceObservation
	var macs []string

	// Processors
	if procURL := odataID(sys["Processors"]); procURL != "" {
		if col, err := get(procURL); err == nil {
			for i, pPath := range memberPaths(col) {
				pData, err := get(pPath)
				if err != nil {
					continue
				}
				cModel := strings.TrimSpace(firstNonEmpty(str(pData["Model"]), fmt.Sprintf("CPU %d", i+1)))
				cMfg := normalizeVendor(str(pData["Manufacturer"]))
				cores := intVal(firstNonNil(pData["TotalCores"], pData["TotalEnabledCores"]))
				modules = append(modules, protocol.ModuleObservation{
					Bay:          firstNonEmpty(str(pData["Socket"]), str(pData["Name"]), fmt.Sprintf("CPU %d", i+1)),
					BayType:      "cpu-socket",
					Profile:      "CPU",
					Manufacturer: cMfg,
					Model:        cModel,
					Serial:       identity.NormalizeSerial(str(pData["SerialNumber"])),
					Status:       "active",
					Attributes:   map[string]any{"cores": cores},
				})
			}
		}
	}

	// Memory
	if memURL := odataID(sys["Memory"]); memURL != "" {
		if col, err := get(memURL); err == nil {
			for i, mPath := range memberPaths(col) {
				mData, err := get(mPath)
				if err != nil {
					continue
				}
				capMiB := intVal(mData["CapacityMiB"])
				if capMiB <= 0 {
					continue
				}
				sizeGB := capMiB / 1024
				klass := strings.ToUpper(firstNonEmpty(str(mData["MemoryDeviceType"]), "DDR4"))
				modules = append(modules, protocol.ModuleObservation{
					Bay:          firstNonEmpty(str(mData["DeviceLocator"]), str(mData["Name"]), fmt.Sprintf("DIMM %d", i+1)),
					BayType:      "dimm-" + strings.ToLower(klass),
					Profile:      "Memory",
					Manufacturer: normalizeVendor(str(mData["Manufacturer"])),
					Model:        firstNonEmpty(str(mData["PartNumber"]), str(mData["Name"]), "DIMM"),
					Serial:       identity.NormalizeSerial(str(mData["SerialNumber"])),
					Status:       "active",
					Description:  fmt.Sprintf("%d GB %s", sizeGB, klass),
					Attributes:   map[string]any{"size_gb": sizeGB, "class": klass},
				})
			}
		}
	}

	// Storage Drives
	if stURL := odataID(sys["Storage"]); stURL != "" {
		if col, err := get(stURL); err == nil {
			seenDrives := make(map[string]bool)
			for _, ctrlPath := range memberPaths(col) {
				ctrl, err := get(ctrlPath)
				if err != nil {
					continue
				}
				drives, _ := ctrl["Drives"].([]any)
				for _, dRef := range drives {
					dp := odataID(dRef)
					if dp == "" {
						continue
					}
					dData, err := get(dp)
					if err != nil {
						continue
					}
					key := firstNonEmpty(str(dData["SerialNumber"]), str(dData["Id"]), dp)
					if seenDrives[key] {
						continue
					}
					seenDrives[key] = true
					sizeGB := intVal(dData["CapacityBytes"]) / 1_000_000_000
					media := strings.ToUpper(firstNonEmpty(str(dData["MediaType"]), "SSD"))
					modules = append(modules, protocol.ModuleObservation{
						Bay:          fmt.Sprintf("Drive %d", len(seenDrives)),
						BayType:      "drive-2-5",
						Profile:      "Hard disk",
						Manufacturer: normalizeVendor(firstNonEmpty(str(dData["Manufacturer"]), str(dData["Model"]))),
						Model:        firstNonEmpty(str(dData["Model"]), fmt.Sprintf("Drive %d", len(seenDrives))),
						Serial:       identity.NormalizeSerial(str(dData["SerialNumber"])),
						Status:       "active",
						Description:  fmt.Sprintf("%d GB %s", sizeGB, media),
						Attributes:   map[string]any{"size_gb": sizeGB, "type": media},
					})
				}
			}
		}
	}

	// Host EthernetInterfaces
	if ethURL := odataID(sys["EthernetInterfaces"]); ethURL != "" {
		if col, err := get(ethURL); err == nil {
			for i, ePath := range memberPaths(col) {
				eData, err := get(ePath)
				if err != nil {
					continue
				}
				mac, err := identity.NormalizeMAC(str(eData["MACAddress"]))
				if err == nil && !identity.IsInvalidMAC(mac) {
					macs = append(macs, mac)
				}
				ifaces = append(ifaces, protocol.InterfaceObservation{
					Name:      firstNonEmpty(str(eData["Id"]), str(eData["Name"]), fmt.Sprintf("nic%d", i)),
					Type:      "1000base-t",
					Kind:      "physical",
					SpeedMbps: intVal(eData["SpeedMbps"]),
					MAC:       mac,
				})
			}
		}
	}

	// Chassis -> Thermal & Power
	chassisURL := chassisPathOverride
	if chassisURL == "" {
		chassisURL = discoverFirst("Chassis", "/redfish/v1/Chassis/System.Embedded.1")
	}
	if thermal, err := get(chassisURL + "/Thermal"); err == nil {
		if fans, ok := thermal["Fans"].([]any); ok {
			for i, f := range fans {
				fm, ok := f.(map[string]any)
				if !ok {
					continue
				}
				rpm := float64(intVal(fm["Reading"]))
				modules = append(modules, protocol.ModuleObservation{
					Bay:         fmt.Sprintf("Fan %d", i+1),
					BayType:     "fan",
					Profile:     "Fan",
					Manufacturer: mfg,
					Model:       "System Fan",
					Status:      statusFromRedfish(fm["Status"]),
					Description: str(fm["Name"]),
				})
				sensors = append(sensors, protocol.SensorObservation{
					Name: firstNonEmpty(str(fm["Name"]), fmt.Sprintf("Fan %d", i+1)),
					Kind: "fan", Value: rpm, Unit: "RPM", Status: statusFromRedfish(fm["Status"]),
				})
			}
		}
	}
	if power, err := get(chassisURL + "/Power"); err == nil {
		if psus, ok := power["PowerSupplies"].([]any); ok {
			for i, p := range psus {
				pm, ok := p.(map[string]any)
				if !ok {
					continue
				}
				watts := intVal(pm["PowerCapacityWatts"])
				modules = append(modules, protocol.ModuleObservation{
					Bay:          fmt.Sprintf("PSU %d", i+1),
					BayType:      "power-supply",
					Profile:      "Power supply",
					Manufacturer: normalizeVendor(firstNonEmpty(str(pm["Manufacturer"]), mfg)),
					Model:        firstNonEmpty(str(pm["Model"]), "Power Supply"),
					Serial:       identity.NormalizeSerial(str(pm["SerialNumber"])),
					Status:       statusFromRedfish(pm["Status"]),
					Description:  fmt.Sprintf("%d W", watts),
					Attributes:   map[string]any{"wattage": watts},
				})
			}
		}
	}

	// BMC / Manager interface
	if mgrURL := discoverFirst("Managers", ""); mgrURL != "" {
		if mgrEthURL := mgrURL + "/EthernetInterfaces"; mgrEthURL != "" {
			if col, err := get(mgrEthURL); err == nil {
				for _, mp := range memberPaths(col) {
					mData, err := get(mp)
					if err != nil {
						continue
					}
					bmcMAC, _ := identity.NormalizeMAC(str(mData["MACAddress"]))
					if bmcMAC != "" && !identity.IsInvalidMAC(bmcMAC) {
						macs = append(macs, bmcMAC)
					}
					ifaces = append(ifaces, protocol.InterfaceObservation{
						Name:        "iDRAC",
						Type:        "1000base-t",
						Kind:        "oob",
						MgmtOnly:    true,
						MAC:         bmcMAC,
						IPAddresses: []protocol.IPObservation{{Address: ip + "/24", DNSName: identity.Slugify("bmc-" + hostName)}},
					})
					break
				}
			}
		}
	}

	claims := protocol.IdentityClaims{
		MACs:     macs,
		Locators: map[string]string{"redfish_ip": ip, "oob_ip": ip},
	}
	if sysUUID != "" {
		claims.UUIDs = []string{sysUUID}
	}
	if serial != "" {
		claims.Serials = []string{serial}
	}
	if hostName != "" && !isGenericRedfishHostname(hostName) {
		claims.Hostnames = []string{hostName}
	}

	status := "active"
	if powerState == "off" {
		status = "offline"
	}

	return protocol.DeviceObservation{
		ObsKey:          ip,
		Site:            site,
		Name:            hostName,
		RoleHint:        "server",
		Manufacturer:    mfg,
		Model:           mdl,
		Status:          status,
		Serial:          serial,
		ServiceTag:      serial,
		SystemUUID:      sysUUID,
		PowerState:      powerState,
		FirmwareVersion: biosVer,
		OOBIP:           ip,
		Claims:          claims,
		Interfaces:      ifaces,
		Modules:         modules,
		Sensors:         sensors,
	}, nil
}

func odataID(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m["@odata.id"].(string)
	return s
}

func memberPaths(col map[string]any) []string {
	members, _ := col["Members"].([]any)
	var out []string
	for _, m := range members {
		if p := odataID(m); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func statusFromRedfish(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return "active"
	}
	if strings.EqualFold(str(m["State"]), "Absent") {
		return "inventory"
	}
	if strings.EqualFold(str(m["Health"]), "Critical") {
		return "failed"
	}
	return "active"
}

func normalizeVendor(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(l, "dell"):
		return "Dell"
	case strings.Contains(l, "hewlett"), strings.Contains(l, "hpe"), strings.Contains(l, "hp"):
		return "HPE"
	case strings.Contains(l, "supermicro"):
		return "Supermicro"
	case strings.Contains(l, "lenovo"), strings.Contains(l, "ibm"):
		return "Lenovo"
	case strings.Contains(l, "intel"):
		return "Intel"
	case strings.Contains(l, "amd"):
		return "AMD"
	case strings.Contains(l, "micron"):
		return "Micron"
	case strings.Contains(l, "samsung"):
		return "Samsung"
	case strings.Contains(l, "hynix"):
		return "SK hynix"
	case strings.Contains(l, "seagate") || strings.HasPrefix(l, "st"):
		return "Seagate"
	case strings.Contains(l, "western") || strings.HasPrefix(l, "wd"):
		return "Western Digital"
	default:
		return strings.TrimSpace(s)
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func isGenericRedfishHostname(raw string) bool {
	l := strings.ToLower(strings.TrimSpace(raw))
	l = strings.NewReplacer(".", "", "-", "", "_", "", " ", "").Replace(l)
	switch l {
	case "", "dellinc", "dell", "localhost", "localhostlocaldomain", "idrac":
		return true
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && v != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstNonNil(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

func intVal(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	default:
		return 0
	}
}
