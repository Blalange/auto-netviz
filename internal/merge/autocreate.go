package merge

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/manual"
	"github.com/blalange/auto-netviz/internal/model"
)

// shouldAutoCreate checks rules.auto_create and rules.ignore for an unmatched observation cluster.
func shouldAutoCreate(cluster identityCluster, rules manual.EffectiveRules) bool {
	if len(cluster.Obs) == 0 {
		return false
	}

	// Check ignore rules across all observations in cluster.
	for _, o := range cluster.Obs {
		d := o.Data
		for _, ipStr := range []string{d.PrimaryIP, d.MgmtIP, d.OOBIP} {
			if ipStr == "" {
				continue
			}
			addr, err := netip.ParseAddr(identity.NormalizeIP(ipStr))
			if err != nil {
				continue
			}
			for _, cidr := range rules.IgnoreCIDRs {
				if pfx, err := netip.ParsePrefix(cidr); err == nil && pfx.Contains(addr) {
					return false
				}
			}
		}
		for _, mac := range d.Claims.MACs {
			norm, err := identity.NormalizeMAC(mac)
			if err != nil {
				continue
			}
			for _, pfx := range rules.IgnoreMACPrefixes {
				if strings.HasPrefix(norm, strings.ToLower(strings.TrimSpace(pfx))) {
					return false
				}
			}
		}
		for _, h := range rules.IgnoreHostnames {
			if strings.EqualFold(strings.TrimSpace(d.Name), strings.TrimSpace(h)) {
				return false
			}
		}
	}

	// Determine category of cluster.
	isClient := true
	isWireless := false
	isInfra := false
	isServer := false
	for _, o := range cluster.Obs {
		d := o.Data
		if !d.IsClient {
			isClient = false
		}
		if d.IsWireless {
			isWireless = true
		}
		switch d.RoleHint {
		case "switch", "gateway", "router", "access-point", "firewall":
			isInfra = true
		case "server", "control-plane", "worker", "storage":
			isServer = true
		}
		switch o.Source {
		case "redfish", "ipmi", "hostos", "kubernetes":
			isServer = true
			isClient = false
		case "snmp", "lldp":
			isInfra = true
			isClient = false
		}
	}

	if isWireless && isClient {
		return rules.AutoWirelessClients
	}
	if isClient {
		return rules.AutoWiredClients
	}
	if isInfra {
		return rules.AutoNetworkDevices
	}
	if isServer {
		return rules.AutoServers
	}
	return rules.AutoNetworkDevices || rules.AutoServers
}

func inferAutoDeviceName(cluster identityCluster, existingNames map[string]struct{}) string {
	// Source preference for naming an auto-created device.
	priority := map[string]int{
		"unifi":      1,
		"kubernetes": 2,
		"hostos":     3,
		"snmp":       4,
		"redfish":    5,
		"ipmi":       6,
		"lldp":       7,
		"discovery":  8,
	}
	sortedObs := append([]SourceDeviceObs(nil), cluster.Obs...)
	sort.SliceStable(sortedObs, func(i, j int) bool {
		pi, pj := priority[sortedObs[i].Source], priority[sortedObs[j].Source]
		if pi == 0 {
			pi = 99
		}
		if pj == 0 {
			pj = 99
		}
		return pi < pj
	})

	candidate := ""
	for _, o := range sortedObs {
		if name := strings.TrimSpace(o.Data.Name); name != "" {
			candidate = name
			break
		}
		for _, h := range o.Data.Claims.Hostnames {
			if strings.TrimSpace(h) != "" {
				candidate = strings.TrimSpace(h)
				break
			}
		}
		if candidate != "" {
			break
		}
	}

	if candidate == "" {
		vendor := inferManufacturer(sortedObs)
		role := inferRole(sortedObs)
		suffix := shortHash(cluster.CanonicalID)[:4]
		for _, o := range sortedObs {
			for _, mac := range o.Data.Claims.MACs {
				if norm, err := identity.NormalizeMAC(mac); err == nil {
					suffix = strings.ReplaceAll(norm[len(norm)-5:], ":", "")
					break
				}
			}
			if suffix != "" {
				break
			}
		}
		if vendor != "" {
			candidate = fmt.Sprintf("%s-%s-%s", identity.Slugify(vendor), role, suffix)
		} else {
			candidate = fmt.Sprintf("%s-%s", role, suffix)
		}
	}

	base := candidate
	i := 2
	for {
		if _, dup := existingNames[candidate]; !dup {
			existingNames[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
		i++
	}
}

func inferManufacturer(obs []SourceDeviceObs) string {
	order := []string{"redfish", "hostos", "ipmi", "unifi", "snmp", "lldp", "kubernetes", "discovery"}
	for _, src := range order {
		for _, o := range obs {
			if o.Source == src && strings.TrimSpace(o.Data.Manufacturer) != "" {
				return strings.TrimSpace(o.Data.Manufacturer)
			}
		}
	}
	for _, o := range obs {
		for _, m := range o.Data.Claims.MACs {
			if v := identity.LookupVendor(m); v != "" {
				return v
			}
		}
		for _, iface := range o.Data.Interfaces {
			if v := identity.LookupVendor(iface.MAC); v != "" {
				return v
			}
		}
	}
	return ""
}

func inferRole(obs []SourceDeviceObs) string {
	order := []string{"unifi", "kubernetes", "redfish", "hostos", "ipmi", "snmp", "lldp", "discovery"}
	for _, src := range order {
		for _, o := range obs {
			if o.Source == src && strings.TrimSpace(o.Data.RoleHint) != "" {
				return strings.TrimSpace(o.Data.RoleHint)
			}
		}
	}
	for _, o := range obs {
		if o.Source == "redfish" || o.Source == "ipmi" || o.Source == "hostos" {
			return "server"
		}
		if o.Source == "snmp" {
			return "switch"
		}
	}
	return "unknown"
}

// synthesizeDeviceType generates an automatic DeviceType with a parametric SVG faceplate
// when no manual device_type is referenced.
func synthesizeDeviceType(dev model.Device) model.DeviceType {
	mfg := dev.Manufacturer
	if mfg == "" {
		mfg = "Generic"
	}
	mdl := dev.Model
	if mdl == "" {
		mdl = fmt.Sprintf("Auto %s", strings.Title(dev.Role)) //nolint:staticcheck
	}
	dtID := dev.DeviceType
	if dtID == "" {
		dtID = identity.Slugify(mfg + "-" + mdl)
		if dtID == "" {
			dtID = "auto-" + dev.ID
		}
	}

	var front, rear []model.FaceplateElement
	var rj45Names, sfpNames, mgmtNames []string
	for _, iface := range dev.Interfaces {
		if iface.Kind == "virtual" || iface.Kind == "wireless" || iface.Kind == "lag" {
			continue
		}
		if iface.MgmtOnly {
			mgmtNames = append(mgmtNames, iface.Name)
			continue
		}
		lowerType := strings.ToLower(iface.Type)
		if strings.Contains(lowerType, "sfp") || strings.Contains(lowerType, "qsfp") {
			sfpNames = append(sfpNames, iface.Name)
		} else {
			rj45Names = append(rj45Names, iface.Name)
		}
	}

	var driveNames, psuNames []string
	for _, mod := range dev.Modules {
		switch strings.ToLower(mod.Profile) {
		case "hard disk":
			driveNames = append(driveNames, mod.Bay)
		case "power supply":
			psuNames = append(psuNames, mod.Bay)
		}
	}

	isNetwork := dev.Role == "switch" || dev.Role == "gateway" || dev.Role == "router" || dev.Role == "access-point"
	if isNetwork {
		front = append(front, model.FaceplateElement{Kind: "lcd", Label: strings.ToUpper(dev.Role[:min(3, len(dev.Role))]), Width: 2})
		if len(rj45Names) > 0 {
			rows := 1
			if len(rj45Names) >= 8 {
				rows = 2
			}
			cols := (len(rj45Names) + rows - 1) / rows
			front = append(front, model.FaceplateElement{
				Kind:      "port_grid",
				Connector: "rj45",
				Rows:      rows,
				Cols:      cols,
				Names:     rj45Names,
			})
		}
		if len(sfpNames) > 0 {
			rows := 1
			if len(sfpNames) >= 2 {
				rows = 2
			}
			cols := (len(sfpNames) + rows - 1) / rows
			front = append(front, model.FaceplateElement{
				Kind:      "port_grid",
				Connector: "sfpp",
				Rows:      rows,
				Cols:      cols,
				Names:     sfpNames,
			})
		}
		rear = append(rear, model.FaceplateElement{Kind: "power", Connector: "iec-c14", Count: 1})
	} else {
		if len(driveNames) > 0 {
			front = append(front, model.FaceplateElement{
				Kind:       "drive_bays",
				FormFactor: "2.5",
				Count:      len(driveNames),
				Names:      driveNames,
			})
		} else {
			front = append(front, model.FaceplateElement{Kind: "label", Label: mdl})
		}
		if len(psuNames) > 0 {
			rear = append(rear, model.FaceplateElement{
				Kind:  "psu_bays",
				Count: len(psuNames),
				Names: psuNames,
			})
		}
		for _, m := range mgmtNames {
			rear = append(rear, model.FaceplateElement{Kind: "mgmt_port", Label: m, Names: []string{m}})
		}
		if len(rj45Names) > 0 {
			rear = append(rear, model.FaceplateElement{
				Kind:      "port_grid",
				Connector: "rj45",
				Rows:      1,
				Cols:      len(rj45Names),
				Names:     rj45Names,
			})
		}
		if len(sfpNames) > 0 {
			rear = append(rear, model.FaceplateElement{
				Kind:      "port_grid",
				Connector: "sfpp",
				Rows:      1,
				Cols:      len(sfpNames),
				Names:     sfpNames,
			})
		}
	}

	return model.DeviceType{
		ID:            dtID,
		Manufacturer:  mfg,
		Model:         mdl,
		UHeight:       1,
		Depth:         "full",
		RoleHint:      dev.Role,
		AutoGenerated: true,
		Faceplate:     &model.FaceplateSpec{Front: front, Rear: rear},
		Sources:       append([]string(nil), dev.Sources...),
	}
}
