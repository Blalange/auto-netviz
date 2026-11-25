package unifi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/blalange/auto-netviz/internal/collector/config"
	"github.com/blalange/auto-netviz/internal/collector/sources"
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/protocol"
)

type Source struct {
	Profiles []config.UnifiProfile
	Defaults config.DefaultsConfig
}

func (s *Source) Name() string { return "unifi" }

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
	var discoveredForSNMP []sources.UniFiDiscoveredTarget

	for _, p := range s.Profiles {
		if !config.IsEnabled(p.Enabled) {
			continue
		}
		start := time.Now()
		h := protocol.SourceHealthPayload{
			Source:    "unifi",
			Profile:   p.Name,
			Enabled:   true,
			LastRunAt: &start,
		}

		baseURL, err := p.URL.Resolve(ctx, rt.BaseDir, rt.K8s, "")
		if err != nil || strings.TrimSpace(baseURL) == "" {
			h.Status = "error"
			h.Error = fmt.Sprintf("resolve url: %v", err)
			h.DurationMs = time.Since(start).Milliseconds()
			healths = append(healths, h)
			continue
		}
		apiKey, _ := p.APIKey.Resolve(ctx, rt.BaseDir, rt.K8s, "")
		user, errU := p.Username.Resolve(ctx, rt.BaseDir, rt.K8s, "")
		pass, errP := p.Password.Resolve(ctx, rt.BaseDir, rt.K8s, "")
		if apiKey == "" && (errU != nil || errP != nil || user == "" || pass == "") {
			h.Status = "error"
			h.Error = "missing UniFi credentials or api_key"
			h.DurationMs = time.Since(start).Milliseconds()
			healths = append(healths, h)
			continue
		}

		timeout := config.ParseDuration(p.Timeout, 30*time.Second)
		site := p.Site
		if site == "" {
			site = rt.Site
		}
		unifiSite := p.UniFiSite
		if unifiSite == "" {
			unifiSite = "default"
		}

		rawDevs, rawNets, rawClients, err := fetchUniFiData(ctx, strings.TrimRight(baseURL, "/"), unifiSite, apiKey, user, pass, p.VerifyTLS, timeout)
		if err != nil {
			h.Status = "error"
			h.Error = err.Error()
			h.DurationMs = time.Since(start).Milliseconds()
			healths = append(healths, h)
			rt.Log.Warn("unifi collection failed", "profile", p.Name, "err", err)
			continue
		}

		models := mergeMaps(s.Defaults.UnifiModels, p.Models)
		vlanNames := mergeIntMaps(s.Defaults.UnifiVLANNames, p.VLANNames)
		includeClients := config.IsEnabled(p.IncludeClients)

		batch, snmpTargets := BuildBatch(rt.CollectorID, site, p.Name, p.SNMPCredential, rawDevs, rawNets, rawClients, models, vlanNames, includeClients)
		discoveredForSNMP = append(discoveredForSNMP, snmpTargets...)
		batches = append(batches, batch)

		done := time.Now()
		h.Status = "ok"
		h.LastSuccessAt = &done
		h.DurationMs = time.Since(start).Milliseconds()
		h.DeviceCount = len(batch.Devices)
		h.LinkCount = len(batch.Links)
		healths = append(healths, h)
	}

	if rt.Shared != nil {
		rt.Shared.SetUniFi(discoveredForSNMP)
	}
	return batches, healths, nil
}

func fetchUniFiData(
	ctx context.Context,
	baseURL, unifiSite, apiKey, username, password string,
	verifyTLS bool,
	timeout time.Duration,
) ([]map[string]any, []map[string]any, []map[string]any, error) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS}, //nolint:gosec
		},
	}

	prefix := "/proxy/network/api/s/" + unifiSite
	if apiKey == "" {
		loginBody, _ := json.Marshal(map[string]any{
			"username": username,
			"password": password,
			"remember": true,
		})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/auth/login", bytes.NewReader(loginBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("login request: %w", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			// Legacy UniFi Controller endpoint fallback
			req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/login", bytes.NewReader(loginBody))
			req2.Header.Set("Content-Type", "application/json")
			resp2, err2 := client.Do(req2)
			if err2 != nil {
				return nil, nil, nil, fmt.Errorf("legacy login: %w", err2)
			}
			_, _ = io.Copy(io.Discard, resp2.Body)
			_ = resp2.Body.Close()
			if resp2.StatusCode != http.StatusOK {
				return nil, nil, nil, fmt.Errorf("unifi legacy login HTTP %d", resp2.StatusCode)
			}
			prefix = "/api/s/" + unifiSite
		} else if resp.StatusCode != http.StatusOK {
			return nil, nil, nil, fmt.Errorf("unifi login HTTP %d", resp.StatusCode)
		}
	}

	getJSONData := func(path string) ([]map[string]any, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+prefix+path, nil)
		if apiKey != "" {
			req.Header.Set("X-API-KEY", apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s HTTP %d", path, resp.StatusCode)
		}
		var payload struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return nil, err
		}
		return payload.Data, nil
	}

	devs, err := getJSONData("/stat/device")
	if err != nil {
		return nil, nil, nil, err
	}
	nets, err := getJSONData("/rest/networkconf")
	if err != nil {
		return nil, nil, nil, err
	}
	clients, err := getJSONData("/stat/sta")
	if err != nil {
		return nil, nil, nil, err
	}
	return devs, nets, clients, nil
}

// BuildBatch converts raw UniFi API payloads into normalized ObservationBatch + SNMP target hints.
func BuildBatch(
	collectorID, site, profile, snmpCred string,
	rawDevs, rawNets, rawClients []map[string]any,
	models map[string]string,
	vlanNames map[int]string,
	includeClients bool,
) (protocol.ObservationBatch, []sources.UniFiDiscoveredTarget) {
	batch := protocol.ObservationBatch{
		BatchID:      fmt.Sprintf("unifi-%s-%d", profile, time.Now().UnixNano()),
		CollectorID:  collectorID,
		Site:         site,
		Source:       "unifi",
		Profile:      profile,
		CollectedAt:  time.Now().UTC(),
		TTLSeconds:   600,
		FullSnapshot: true,
	}

	// Pre-collect gateway IPs from corporate/VLAN networks so the gateway device merges all its subnet IPs.
	type netGateway struct {
		cidr string
		ip   string
		name string
	}
	var gatewaySubnets []netGateway
	seenGwCIDR := make(map[string]bool)
	for _, net := range rawNets {
		purpose := strings.ToLower(str(net["purpose"]))
		if purpose != "" && purpose != "corporate" {
			continue
		}
		subnet := str(net["ip_subnet"])
		if subnet == "" {
			continue
		}
		if pfx, err := netip.ParsePrefix(subnet); err == nil {
			ipStr := pfx.Addr().String()
			cidr := fmt.Sprintf("%s/%d", ipStr, pfx.Bits())
			if !seenGwCIDR[cidr] {
				seenGwCIDR[cidr] = true
				gatewaySubnets = append(gatewaySubnets, netGateway{
					cidr: cidr,
					ip:   ipStr,
					name: firstNonEmpty(str(net["name"]), "LAN"),
				})
			}
		}
	}

	// Build unique names per MAC
	nameCounts := make(map[string]int)
	for _, d := range rawDevs {
		rawName := firstNonEmpty(str(d["name"]), str(d["hostname"]))
		if rawName != "" {
			nameCounts[rawName]++
		}
	}
	namesByMAC := make(map[string]string)
	byMAC := make(map[string]map[string]any)
	var snmpTargets []sources.UniFiDiscoveredTarget

	for _, d := range rawDevs {
		mac, _ := identity.NormalizeMAC(str(d["mac"]))
		rawName := firstNonEmpty(str(d["name"]), str(d["hostname"]))
		if rawName == "" && len(mac) >= 5 {
			rawName = "UniFi-" + strings.ReplaceAll(mac[len(mac)-5:], ":", "")
		}
		if nameCounts[rawName] > 1 && len(mac) >= 5 {
			rawName = fmt.Sprintf("%s (%s)", rawName, strings.ReplaceAll(mac[len(mac)-5:], ":", ""))
		}
		if mac != "" {
			namesByMAC[mac] = rawName
			byMAC[mac] = d
		}

		role := "switch"
		switch str(d["type"]) {
		case "uap":
			role = "access-point"
		case "udm", "uxg", "ugw":
			role = "gateway"
		case "usw":
			role = "switch"
		}

		status := "offline"
		switch toInt(d["state"]) {
		case 1:
			status = "active"
		case 2:
			status = "inventory"
		}

		modelCode := str(d["model"])
		modelName := modelCode
		if mapped, ok := models[modelCode]; ok && mapped != "" {
			modelName = mapped
			modelCode = mapped
		}
		serial := identity.NormalizeSerial(str(d["serial"]))
		mgmtIP := firstNonEmpty(str(d["lan_ip"]), str(d["ip"]), nestedStr(d["config_network"], "ip"))

		var ifaces []protocol.InterfaceObservation
		portTable, _ := d["port_table"].([]any)
		for _, pt := range portTable {
			pm, ok := pt.(map[string]any)
			if !ok {
				continue
			}
			idx := toInt(pm["port_idx"])
			if idx <= 0 {
				continue
			}
			speed := toInt(pm["speed"])
			up := toBool(pm["up"])
			enabled := true
			if en, exists := pm["enable"]; exists {
				enabled = toBool(en)
			}
			iface := protocol.InterfaceObservation{
				Name:        fmt.Sprintf("Port %d", idx),
				Type:        unifiInterfaceType(pm),
				Kind:        "physical",
				Enabled:     &enabled,
				LinkUp:      &up,
				SpeedMbps:   speed,
				Description: str(pm["name"]),
			}
			if toBool(pm["port_poe"]) {
				iface.PoEMode = "pse"
				iface.PoEWatts = toFloat(pm["poe_power"])
			}
			ifaces = append(ifaces, iface)
		}

		if len(ifaces) == 0 {
			ifaces = append(ifaces, protocol.InterfaceObservation{
				Name: "Port 1", Type: "1000base-t", Kind: "physical", MAC: mac,
			})
		}

		if role == "gateway" {
			var lanIPs []protocol.IPObservation
			lanIP := str(d["lan_ip"])
			if lanIP != "" {
				lanIPs = append(lanIPs, protocol.IPObservation{Address: lanIP + "/24", Primary: true})
			}
			for _, gw := range gatewaySubnets {
				if gw.ip == "" || gw.ip == lanIP {
					continue
				}
				lanIPs = append(lanIPs, protocol.IPObservation{
					Address:     gw.cidr,
					Description: "Gateway: " + gw.name,
				})
			}
			if len(lanIPs) > 0 {
				ifaces = append([]protocol.InterfaceObservation{{
					Name: "lan", Type: "virtual", Kind: "virtual", MAC: mac, IPAddresses: lanIPs,
				}}, ifaces...)
			}
			if wan, ok := d["wan1"].(map[string]any); ok {
				wanIP := str(wan["ip"])
				wanMask := str(wan["netmask"])
				if wanIP != "" && wanMask != "" {
					if cidr := ipAndMaskToCIDR(wanIP, wanMask); cidr != "" {
						for i := range ifaces {
							if ifaces[i].Name == "Port 9" {
								ifaces[i].Description = "WAN uplink"
								ifaces[i].IPAddresses = []protocol.IPObservation{{Address: cidr, Description: "WAN"}}
								break
							}
						}
					}
				}
			}
		} else if mgmtIP != "" && len(ifaces) > 0 {
			targetIdx := 0
			if upl, ok := d["uplink"].(map[string]any); ok {
				if pIdx := toInt(upl["port_idx"]); pIdx > 0 {
					pname := fmt.Sprintf("Port %d", pIdx)
					for i := range ifaces {
						if ifaces[i].Name == pname {
							targetIdx = i
							break
						}
					}
				}
			}
			ifaces[targetIdx].MAC = mac
			ifaces[targetIdx].IPAddresses = append(ifaces[targetIdx].IPAddresses, protocol.IPObservation{
				Address: mgmtIP + "/24",
				DNSName: identity.Slugify(rawName),
				Primary: true,
			})
		}

		if role == "access-point" {
			if radios, ok := d["radio_table"].([]any); ok {
				for _, r := range radios {
					rm, ok := r.(map[string]any)
					if !ok {
						continue
					}
					rName := firstNonEmpty(str(rm["name"]), "wifi")
					ifaces = append(ifaces, protocol.InterfaceObservation{
						Name: rName, Type: "ieee802.11ax", Kind: "wireless",
						Description: str(rm["radio"]) + " radio",
					})
				}
			}
		}

		claims := protocol.IdentityClaims{
			Hostnames: []string{rawName},
			Locators:  map[string]string{},
		}
		if serial != "" {
			claims.Serials = []string{serial}
		}
		if mac != "" {
			claims.MACs = []string{mac}
		}
		if mgmtIP != "" {
			claims.Locators["unifi_ip"] = mgmtIP
			claims.Locators["snmp_ip"] = mgmtIP
		}

		batch.Devices = append(batch.Devices, protocol.DeviceObservation{
			ObsKey:          firstNonEmpty(mac, rawName),
			Site:            site,
			Name:            rawName,
			RoleHint:        role,
			Manufacturer:    "Ubiquiti",
			Model:           modelName,
			ModelCode:       modelCode,
			Platform:        "unifi",
			Status:          status,
			Serial:          serial,
			FirmwareVersion: str(d["version"]),
			PrimaryIP:       mgmtIP,
			MgmtIP:          mgmtIP,
			Claims:          claims,
			Interfaces:      ifaces,
		})

		if status == "active" && mgmtIP != "" {
			snmpTargets = append(snmpTargets, sources.UniFiDiscoveredTarget{
				Site:           site,
				Name:           rawName,
				IP:             mgmtIP,
				MAC:            mac,
				Serial:         serial,
				SNMPCredential: snmpCred,
			})
		}
	}

	// Uplinks between UniFi devices
	for _, d := range rawDevs {
		childMAC, _ := identity.NormalizeMAC(str(d["mac"]))
		childName := namesByMAC[childMAC]
		upl, ok := d["uplink"].(map[string]any)
		if !ok {
			continue
		}
		parentMAC, _ := identity.NormalizeMAC(str(upl["uplink_mac"]))
		parentName, ok := namesByMAC[parentMAC]
		if !ok {
			continue
		}
		localPortIdx := toInt(upl["port_idx"])
		remotePortIdx := toInt(upl["uplink_remote_port"])
		if localPortIdx <= 0 || remotePortIdx <= 0 {
			continue
		}
		speed := toInt(upl["speed"])
		cType := "cat6"
		if speed >= 10000 {
			cType = "dac-passive"
		} else if speed > 1000 {
			cType = "cat6a"
		}
		batch.Links = append(batch.Links, protocol.LinkObservation{
			ObsKey:           fmt.Sprintf("uplink:%s:%d->%s:%d", childMAC, localPortIdx, parentMAC, remotePortIdx),
			Site:             site,
			Method:           "unifi_uplink",
			LocalObsKey:      childMAC,
			LocalDeviceName:  childName,
			LocalMAC:         childMAC,
			LocalPort:        fmt.Sprintf("Port %d", localPortIdx),
			RemoteObsKey:     parentMAC,
			RemoteDeviceName: parentName,
			RemoteMAC:        parentMAC,
			RemotePort:       fmt.Sprintf("Port %d", remotePortIdx),
			CableType:        cType,
			SpeedMbps:        speed,
			Description:      fmt.Sprintf("UniFi uplink to %s Port %d", parentName, remotePortIdx),
		})
	}

	// Networks -> VLANs + Prefixes
	for _, net := range rawNets {
		netName := strings.TrimSpace(str(net["name"]))
		var vid int
		if v, exists := net["vlan"]; exists && v != nil {
			vid = toInt(v)
		} else if netName == "Default" {
			vid = 1
		}
		if vid > 0 {
			if override, ok := vlanNames[vid]; ok && override != "" {
				netName = override
			}
			batch.VLANs = append(batch.VLANs, protocol.VLANObservation{
				Site:        site,
				VID:         vid,
				Name:        netName,
				Description: "UniFi network: " + netName,
			})
		}
		subnet := str(net["ip_subnet"])
		if subnet != "" {
			if pfx, err := netip.ParsePrefix(subnet); err == nil {
				batch.Prefixes = append(batch.Prefixes, protocol.PrefixObservation{
					CIDR:        pfx.Masked().String(),
					Site:        site,
					VLAN:        vid,
					Gateway:     pfx.Addr().String(),
					Purpose:     str(net["purpose"]),
					Description: fmt.Sprintf("UniFi %s", netName),
				})
			}
		}
	}

	// Clients -> Devices + Client Links
	if includeClients {
		for _, cl := range rawClients {
			mac, err := identity.NormalizeMAC(str(cl["mac"]))
			if err != nil {
				continue
			}
			isWired := toBool(cl["is_wired"])
			realName := firstNonEmpty(str(cl["name"]), str(cl["hostname"]))
			cName := firstNonEmpty(realName, str(cl["oui"]))
			ip := str(cl["ip"])
			obsKey := "client:" + mac
			claims := protocol.IdentityClaims{
				MACs:     []string{mac},
				Locators: map[string]string{},
			}
			if realName != "" {
				claims.Hostnames = []string{realName}
			}
			if ip != "" {
				claims.Locators["unifi_ip"] = ip
			}
			var ifaces []protocol.InterfaceObservation
			if ip != "" {
				ifaces = append(ifaces, protocol.InterfaceObservation{
					Name:        "eth0",
					Type:        "1000base-t",
					Kind:        "physical",
					MAC:         mac,
					IPAddresses: []protocol.IPObservation{{Address: ip + "/24", DNSName: identity.Slugify(cName), Primary: true}},
				})
			}
			batch.Devices = append(batch.Devices, protocol.DeviceObservation{
				ObsKey:       obsKey,
				Site:         site,
				Name:         cName,
				RoleHint:     "workstation",
				Manufacturer: identity.LookupVendor(mac),
				Status:       "active",
				PrimaryIP:    ip,
				IsClient:     true,
				IsWireless:   !isWired,
				Claims:       claims,
				Interfaces:   ifaces,
			})
			if isWired {
				swMAC, _ := identity.NormalizeMAC(str(cl["sw_mac"]))
				swPort := toInt(cl["sw_port"])
				if swMAC != "" && swPort > 0 {
					batch.Links = append(batch.Links, protocol.LinkObservation{
						ObsKey:           fmt.Sprintf("client:%s->%s:%d", mac, swMAC, swPort),
						Site:             site,
						Method:           "unifi_client",
						LocalObsKey:      obsKey,
						LocalDeviceName:  cName,
						LocalMAC:         mac,
						LocalPort:        "eth0",
						RemoteObsKey:     swMAC,
						RemoteDeviceName: namesByMAC[swMAC],
						RemoteMAC:        swMAC,
						RemotePort:       fmt.Sprintf("Port %d", swPort),
						CableType:        "cat6",
					})
				}
			}
		}
	}

	return batch, snmpTargets
}

func unifiInterfaceType(pm map[string]any) string {
	media := strings.ToUpper(str(pm["media"]))
	speed := toInt(pm["speed"])
	switch {
	case strings.Contains(media, "SFP+") || strings.Contains(media, "10G"):
		return "10gbase-x-sfpp"
	case media == "SFP":
		return "1000base-x-sfp"
	case media == "2P5GE" || media == "2.5GE" || speed == 2500:
		return "2.5gbase-t"
	case media == "10GE" || speed >= 10000:
		return "10gbase-t"
	default:
		return "1000base-t"
	}
}

func ipAndMaskToCIDR(ipStr, maskStr string) string {
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return ""
	}
	maskAddr, err := netip.ParseAddr(maskStr)
	if err != nil || !maskAddr.Is4() {
		return ""
	}
	b := maskAddr.As4()
	ones := 0
	for _, octet := range b {
		for bit := 7; bit >= 0; bit-- {
			if (octet & (1 << bit)) != 0 {
				ones++
			}
		}
	}
	return fmt.Sprintf("%s/%d", addr.String(), ones)
}

func mergeMaps(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func mergeIntMaps(a, b map[int]string) map[int]string {
	out := make(map[int]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func str(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func nestedStr(v any, key string) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	return str(m[key])
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

func toBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
