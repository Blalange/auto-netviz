package merge

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blalange/auto-netviz/internal/db"
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/manual"
	"github.com/blalange/auto-netviz/internal/model"
	"github.com/blalange/auto-netviz/internal/protocol"
	"gopkg.in/yaml.v3"
)

var roleColors = map[string]string{
	"gateway":       "#f59e0b",
	"router":        "#f59e0b",
	"switch":        "#0ea5e9",
	"access-point":  "#8b5cf6",
	"control-plane": "#3b82f6",
	"worker":        "#60a5fa",
	"server":        "#64748b",
	"storage":       "#a855f7",
	"power":         "#f97316",
	"appliance":     "#14b8a6",
	"workstation":   "#ec4899",
	"unknown":       "#94a3b8",
}

var sourcePrecedenceIdentity = []string{
	"manual", "redfish", "hostos", "ipmi", "unifi", "snmp", "kubernetes", "lldp", "discovery",
}

var sourcePrecedencePort = []string{
	"manual", "unifi", "snmp", "hostos", "redfish", "lldp", "kubernetes", "discovery",
}

// MergeResult contains the unified CanonicalState plus updated persistent entity identities.
type MergeResult struct {
	State      *model.CanonicalState
	Identities []db.IdentityRecord
}

// Merge combines the manual YAML inventory and all active collector observations
// into a deterministic CanonicalState.
func Merge(inv *manual.Inventory, rawObs []db.StoredObservation, prevIdentities []db.IdentityRecord) (*MergeResult, error) {
	now := time.Now().UTC()
	if inv == nil {
		inv = &manual.Inventory{RackPlans: make(map[string]manual.RackPlan)}
	}

	devObs, linkObs, vlanObs, prefixObs := decodeObservations(rawObs)

	clusters, obsToCanonical, persistedIDs, resolveIssues := resolveDeviceIdentities(inv, devObs, prevIdentities, now)

	state := &model.CanonicalState{
		GeneratedAt: now,
	}
	state.Issues = append(state.Issues, inv.Issues...)
	state.Issues = append(state.Issues, resolveIssues...)

	// 1. Base sites, locations, owners, contacts, device_types, module_types
	siteMap := make(map[string]model.Site)
	for _, s := range inv.Sites {
		siteMap[s.ID] = model.Site{
			ID:          s.ID,
			Name:        s.Name,
			Status:      s.Status,
			Facility:    s.Facility,
			Address:     s.Address,
			Description: s.Description,
			Sources:     []string{"manual"},
		}
	}
	ensureSite := func(id string, src string) string {
		if id == "" {
			id = inv.Defaults.Site
		}
		if id == "" {
			id = "default"
		}
		if s, ok := siteMap[id]; ok {
			s.Sources = addUnique(s.Sources, src)
			siteMap[id] = s
			return id
		}
		siteMap[id] = model.Site{
			ID:      id,
			Name:    strings.ToUpper(id[:1]) + id[1:],
			Status:  "active",
			Sources: []string{src},
		}
		return id
	}

	for _, l := range inv.Locations {
		site := ensureSite(l.Site, "manual")
		state.Locations = append(state.Locations, model.Location{
			ID:          site + "/" + l.ID,
			Site:        site,
			Name:        l.Name,
			Description: l.Description,
			Sources:     []string{"manual"},
		})
	}
	for _, og := range inv.OwnerGroups {
		state.OwnerGroups = append(state.OwnerGroups, model.OwnerGroup{
			ID:          identity.Slugify(og.Name),
			Name:        og.Name,
			Description: og.Description,
			Sources:     []string{"manual"},
		})
	}
	for _, o := range inv.Owners {
		state.Owners = append(state.Owners, model.Owner{
			ID:          identity.Slugify(o.Name),
			Name:        o.Name,
			Group:       o.Group,
			Email:       o.Email,
			Link:        o.Link,
			Description: o.Description,
			Sources:     []string{"manual"},
		})
	}
	for _, c := range inv.Contacts {
		id := identity.Slugify(c.Name + "-" + c.Role + "-" + c.Owner)
		state.Contacts = append(state.Contacts, model.Contact{
			ID:      id,
			Name:    c.Name,
			Role:    c.Role,
			Owner:   c.Owner,
			Email:   c.Email,
			Phone:   c.Phone,
			Notes:   c.Notes,
			Sources: []string{"manual"},
		})
	}

	dtMap := make(map[string]model.DeviceType)
	for _, dt := range inv.DeviceTypes {
		dtMap[dt.ID] = model.DeviceType{
			ID:           dt.ID,
			Manufacturer: dt.Manufacturer,
			Model:        dt.Model,
			UHeight:      dt.UHeight,
			Depth:        dt.Depth,
			RoleHint:     dt.RoleHint,
			PartNumber:   dt.PartNumber,
			Airflow:      dt.Airflow,
			WeightKg:     dt.WeightKg,
			Description:  dt.Description,
			Faceplate:    convertFaceplate(dt.Faceplate),
			Sources:      []string{"manual"},
		}
	}

	mtMap := make(map[string]model.ModuleType)
	for _, mt := range inv.ModuleTypes {
		mtMap[mt.ID] = model.ModuleType{
			ID:           mt.ID,
			Manufacturer: mt.Manufacturer,
			Model:        mt.Model,
			Profile:      mt.Profile,
			BayType:      mt.BayType,
			PartNumber:   mt.PartNumber,
			Description:  mt.Description,
			Attributes:   mt.Attributes,
			Sources:      []string{"manual"},
		}
	}

	// 2. Merge Devices
	usedNames := make(map[string]struct{})
	for _, d := range inv.Devices {
		usedNames[d.Name] = struct{}{}
	}

	devByID := make(map[string]*model.Device)
	devByName := make(map[string]*model.Device)

	for i := range clusters {
		c := clusters[i]
		if c.Manual == nil && !shouldAutoCreate(c, inv.Rules) {
			continue
		}

		dev := mergeDeviceCluster(c, inv, dtMap, mtMap, usedNames, ensureSite, &state.Issues, now)
		state.Devices = append(state.Devices, dev)
	}

	for i := range state.Devices {
		d := &state.Devices[i]
		devByID[d.ID] = d
		devByName[d.Name] = d
	}

	// 3. Merge VLANs (site, vid)
	vlanMap := make(map[string]model.VLAN)
	for _, v := range inv.VLANs {
		site := ensureSite(v.Site, "manual")
		id := fmt.Sprintf("%s:%d", site, v.VID)
		vlanMap[id] = model.VLAN{
			ID:          id,
			Site:        site,
			VID:         v.VID,
			Name:        v.Name,
			Status:      v.Status,
			Color:       v.Color,
			Description: v.Description,
			Sources:     []string{"manual"},
		}
	}
	for _, vo := range vlanObs {
		site := ensureSite(vo.Data.Site, vo.Source)
		if vo.Data.VID <= 0 || vo.Data.VID > 4094 {
			continue
		}
		id := fmt.Sprintf("%s:%d", site, vo.Data.VID)
		if cur, ok := vlanMap[id]; ok {
			cur.Sources = addUnique(cur.Sources, vo.Source)
			if cur.Name == "" || strings.HasPrefix(cur.Name, "VLAN ") {
				if vo.Data.Name != "" {
					cur.Name = vo.Data.Name
				}
			}
			vlanMap[id] = cur
		} else {
			name := vo.Data.Name
			if name == "" {
				name = fmt.Sprintf("VLAN %d", vo.Data.VID)
			}
			vlanMap[id] = model.VLAN{
				ID:          id,
				Site:        site,
				VID:         vo.Data.VID,
				Name:        name,
				Status:      "active",
				Description: vo.Data.Description,
				Sources:     []string{vo.Source},
			}
		}
	}
	// Ensure any VLAN referenced by device interfaces exists.
	for _, d := range state.Devices {
		for _, iface := range d.Interfaces {
			vids := append([]int(nil), iface.TaggedVLANs...)
			if iface.UntaggedVLAN != nil {
				vids = append(vids, *iface.UntaggedVLAN)
			}
			for _, vid := range vids {
				if vid <= 0 || vid > 4094 {
					continue
				}
				id := fmt.Sprintf("%s:%d", d.Site, vid)
				if _, ok := vlanMap[id]; !ok {
					vlanMap[id] = model.VLAN{
						ID:      id,
						Site:    d.Site,
						VID:     vid,
						Name:    fmt.Sprintf("VLAN %d", vid),
						Status:  "active",
						Sources: append([]string(nil), iface.Sources...),
					}
				}
			}
		}
	}

	// 4. Merge Prefixes + compute IP utilization
	pfxMap := make(map[string]model.Prefix)
	for _, p := range inv.Prefixes {
		site := ensureSite(p.Site, "manual")
		cidr := identity.NormalizePrefix(p.CIDR)
		pfxMap[cidr] = model.Prefix{
			ID:          identity.Slugify(cidr),
			CIDR:        cidr,
			Site:        site,
			VLAN:        p.VLAN,
			Gateway:     p.Gateway,
			Status:      p.Status,
			Description: p.Description,
			Sources:     []string{"manual"},
		}
	}
	for _, po := range prefixObs {
		cidr := identity.NormalizePrefix(po.Data.CIDR)
		if cidr == "" {
			continue
		}
		site := ensureSite(po.Data.Site, po.Source)
		if cur, ok := pfxMap[cidr]; ok {
			cur.Sources = addUnique(cur.Sources, po.Source)
			if cur.VLAN == nil && po.Data.VLAN > 0 {
				v := po.Data.VLAN
				cur.VLAN = &v
			}
			if cur.Gateway == "" {
				cur.Gateway = po.Data.Gateway
			}
			pfxMap[cidr] = cur
		} else {
			var vlanPtr *int
			if po.Data.VLAN > 0 {
				v := po.Data.VLAN
				vlanPtr = &v
			}
			pfxMap[cidr] = model.Prefix{
				ID:          identity.Slugify(cidr),
				CIDR:        cidr,
				Site:        site,
				VLAN:        vlanPtr,
				Gateway:     po.Data.Gateway,
				Status:      "active",
				Description: po.Data.Description,
				Sources:     []string{po.Source},
			}
		}
	}

	// Assign Prefix tags & compute IP utilization across all device interface IPs.
	type parsedPfx struct {
		cidr   string
		netPfx netip.Prefix
		used   map[string]struct{}
	}
	var parsedPfxs []parsedPfx
	for cidr := range pfxMap {
		if p, err := netip.ParsePrefix(cidr); err == nil {
			parsedPfxs = append(parsedPfxs, parsedPfx{cidr: cidr, netPfx: p, used: make(map[string]struct{})})
		}
	}
	// Longest prefix match first.
	sort.Slice(parsedPfxs, func(i, j int) bool {
		return parsedPfxs[i].netPfx.Bits() > parsedPfxs[j].netPfx.Bits()
	})
	for di := range state.Devices {
		dev := &state.Devices[di]
		for ii := range dev.Interfaces {
			iface := &dev.Interfaces[ii]
			for ipi := range iface.IPAddresses {
				ipObj := &iface.IPAddresses[ipi]
				addr, err := netip.ParseAddr(ipObj.IP)
				if err != nil {
					continue
				}
				for pi := range parsedPfxs {
					if parsedPfxs[pi].netPfx.Contains(addr) {
						ipObj.Prefix = parsedPfxs[pi].cidr
						parsedPfxs[pi].used[ipObj.IP] = struct{}{}
						break
					}
				}
			}
		}
	}
	for _, pp := range parsedPfxs {
		p := pfxMap[pp.cidr]
		p.UsedIPs = len(pp.used)
		total := prefixHostCapacity(pp.netPfx)
		p.TotalHosts = total
		if total > 0 {
			p.Utilization = float64(p.UsedIPs) / float64(total) * 100
			if p.Utilization > 100 {
				p.Utilization = 100
			}
		}
		pfxMap[pp.cidr] = p
	}

	// 5. Resolve Cables (Manual > LLDP/CDP > UniFi Uplink/Client > SNMP FDB)
	state.Cables = resolveCables(inv, linkObs, devObs, obsToCanonical, devByID, devByName)

	// 6. Build Racks with Slots and Power Draw
	rackMap := make(map[string]model.Rack)
	for _, r := range inv.Racks {
		site := ensureSite(r.Site, "manual")
		rackKey := site + "/" + r.Name
		rackID := identity.Slugify(site + "-" + r.Name)
		rackMap[rackKey] = model.Rack{
			ID:           rackID,
			Name:         r.Name,
			Site:         site,
			Location:     r.Location,
			Status:       r.Status,
			Width:        r.Width,
			UHeight:      r.UHeight,
			DescUnits:    r.DescUnits,
			PowerBudgetW: r.PowerBudgetW,
			Description:  r.Description,
			Sources:      []string{"manual"},
		}
	}

	for di := range state.Devices {
		dev := &state.Devices[di]
		if dev.Rack == "" || dev.Position == nil {
			continue
		}
		rackKey := dev.Site + "/" + dev.Rack
		rack, ok := rackMap[rackKey]
		if !ok {
			continue
		}
		dev.RackID = rack.ID
		uHeight := 1
		if dt, dtOK := dtMap[dev.DeviceType]; dtOK && dt.UHeight > 0 {
			uHeight = dt.UHeight
		}
		endU := *dev.Position + uHeight - 1
		dev.EndPosition = &endU
		face := dev.Face
		if face == "" {
			face = "front"
		}
		rack.Slots = append(rack.Slots, model.RackSlot{
			DeviceID:   dev.ID,
			DeviceName: dev.Name,
			Role:       dev.Role,
			DeviceType: dev.DeviceType,
			StartU:     *dev.Position,
			EndU:       endU,
			UHeight:    uHeight,
			Face:       face,
			Status:     dev.Status,
		})
		rack.UsedU += uHeight
		rackMap[rackKey] = rack
	}

	for k, r := range rackMap {
		sort.Slice(r.Slots, func(i, j int) bool {
			return r.Slots[i].StartU > r.Slots[j].StartU
		})
		rackMap[k] = r
	}

	// 7. Unified Assets (standalone + tracked devices + serialized modules)
	for _, a := range inv.Assets {
		site := ensureSite(a.Site, "manual")
		id := a.AssetTag
		if id == "" {
			id = identity.Slugify(a.Name)
		}
		state.Assets = append(state.Assets, model.Asset{
			ID:            "asset-" + identity.Slugify(id),
			AssetTag:      a.AssetTag,
			Name:          a.Name,
			Kind:          "standalone",
			Category:      a.Category,
			Manufacturer:  a.Manufacturer,
			Model:         a.Model,
			Serial:        a.Serial,
			Status:        a.Status,
			Site:          site,
			Location:      a.Location,
			Owner:         a.Owner,
			PurchaseDate:  a.PurchaseDate,
			WarrantyUntil: a.WarrantyUntil,
			Notes:         a.Notes,
			Custom:        a.Custom,
			Sources:       []string{"manual"},
		})
	}
	for _, d := range state.Devices {
		if d.AssetTag != "" || d.Serial != "" || d.WarrantyUntil != "" {
			state.Assets = append(state.Assets, model.Asset{
				ID:            "asset-dev-" + d.ID,
				AssetTag:      d.AssetTag,
				Name:          d.Name,
				Kind:          "device",
				Category:      d.Role,
				Manufacturer:  d.Manufacturer,
				Model:         d.Model,
				Serial:        d.Serial,
				Status:        d.Status,
				Site:          d.Site,
				Location:      d.Location,
				DeviceID:      d.ID,
				DeviceName:    d.Name,
				Owner:         d.Owner,
				WarrantyUntil: d.WarrantyUntil,
				Sources:       append([]string(nil), d.Sources...),
			})
		}
		for _, mod := range d.Modules {
			if mod.Serial != "" || mod.AssetTag != "" {
				state.Assets = append(state.Assets, model.Asset{
					ID:           "asset-mod-" + mod.ID,
					AssetTag:     mod.AssetTag,
					Name:         fmt.Sprintf("%s — %s", d.Name, mod.Bay),
					Kind:         "module",
					Category:     mod.Profile,
					Manufacturer: mod.Manufacturer,
					Model:        mod.Model,
					Serial:       mod.Serial,
					Status:       mod.Status,
					Site:         d.Site,
					Location:     d.Location,
					DeviceID:     d.ID,
					DeviceName:   d.Name,
					Owner:        d.Owner,
					Sources:      append([]string(nil), mod.Sources...),
				})
			}
		}
	}

	// Collect maps into deterministic sorted slices.
	for _, s := range siteMap {
		state.Sites = append(state.Sites, s)
	}
	for _, dt := range dtMap {
		state.DeviceTypes = append(state.DeviceTypes, dt)
	}
	for _, mt := range mtMap {
		state.ModuleTypes = append(state.ModuleTypes, mt)
	}
	for _, r := range rackMap {
		state.Racks = append(state.Racks, r)
	}
	for _, v := range vlanMap {
		state.VLANs = append(state.VLANs, v)
	}
	for _, p := range pfxMap {
		state.Prefixes = append(state.Prefixes, p)
	}

	sortState(state)
	return &MergeResult{State: state, Identities: persistedIDs}, nil
}

func mergeDeviceCluster(
	c identityCluster,
	inv *manual.Inventory,
	dtMap map[string]model.DeviceType,
	mtMap map[string]model.ModuleType,
	usedNames map[string]struct{},
	ensureSite func(string, string) string,
	issues *[]model.Issue,
	now time.Time,
) model.Device {
	var name, origin string
	if c.Manual != nil {
		name = c.Manual.Name
		if len(c.Obs) > 0 {
			origin = "merged"
		} else {
			origin = "manual"
		}
	} else {
		name = inferAutoDeviceName(c, usedNames)
		origin = "auto"
	}

	dev := model.Device{
		ID:          c.CanonicalID,
		Name:        name,
		Origin:      origin,
		FirstSeenAt: c.FirstSeen,
		LastSeenAt:  c.LastSeen,
	}
	sources := make(map[string]struct{})
	collectors := make(map[string]struct{})
	if c.Manual != nil {
		sources["manual"] = struct{}{}
	}
	for _, o := range c.Obs {
		sources[o.Source] = struct{}{}
		collectors[o.CollectorID] = struct{}{}
	}

	// Site
	siteVal := ""
	if c.Manual != nil && c.Manual.Site != "" {
		siteVal = c.Manual.Site
		dev.Provenance = append(dev.Provenance, model.FieldProvenance{Field: "site", Source: "manual", ManualOverride: true})
	} else {
		for _, o := range c.Obs {
			if o.Data.Site != "" {
				siteVal = o.Data.Site
				dev.Provenance = append(dev.Provenance, model.FieldProvenance{Field: "site", Source: o.Source, CollectorID: o.CollectorID, ObservedAt: o.ObservedAt})
				break
			}
			if o.Site != "" {
				siteVal = o.Site
			}
		}
	}
	dev.Site = ensureSite(siteVal, firstSourceOrManual(c))

	// Owner, Contacts, AssetTag, Location, Rack, Position, Face, Warranty, Custom, Tags
	if c.Manual != nil {
		m := c.Manual
		dev.Location = m.Location
		dev.Owner = m.Owner
		dev.AssetTag = m.AssetTag
		dev.WarrantyUntil = m.WarrantyUntil
		dev.Rack = m.Rack
		dev.Position = m.Position
		dev.Face = m.Face
		dev.Comments = m.Comments
		dev.Custom = m.Custom
		dev.Tags = append([]string(nil), m.Tags...)
		for _, ct := range m.Contacts {
			dev.Contacts = append(dev.Contacts, model.DeviceContact{Contact: ct.Contact, Role: ct.Role})
		}
	}
	if dev.Owner == "" && inv.Defaults.Owner != "" {
		dev.Owner = inv.Defaults.Owner
	}

	// Helper to pick prioritized field from observations & detect drift vs manual.
	pickObs := func(precedence []string, allow func(SourceDeviceObs) bool, getter func(SourceDeviceObs) string) (bestVal, bestSrc, bestCol string, bestAt time.Time, bestIsClient bool) {
		for _, src := range precedence {
			if src == "manual" {
				continue
			}
			for _, o := range c.Obs {
				if o.Source != src {
					continue
				}
				if allow != nil && !allow(o) {
					continue
				}
				if v := strings.TrimSpace(getter(o)); v != "" {
					return v, o.Source, o.CollectorID, o.ObservedAt, o.Data.IsClient
				}
			}
		}
		return "", "", "", time.Time{}, false
	}
	pickStrCustom := func(field, manualVal string, precedence []string, allow func(SourceDeviceObs) bool, getter func(SourceDeviceObs) string, suppressDrift func(manualVal, liveVal, liveSrc string, liveIsClient bool) bool) string {
		bestVal, bestSrc, bestCol, bestAt, bestIsClient := pickObs(precedence, allow, getter)
		if bestVal == "" && allow != nil {
			bestVal, bestSrc, bestCol, bestAt, bestIsClient = pickObs(precedence, nil, getter)
		}
		if manualVal != "" {
			diff := bestVal != "" && !strings.EqualFold(manualVal, bestVal)
			suppressed := suppressDrift != nil && diff && suppressDrift(manualVal, bestVal, bestSrc, bestIsClient)
			dev.Provenance = append(dev.Provenance, model.FieldProvenance{
				Field:          field,
				Source:         "manual",
				ManualOverride: diff,
			})
			if diff && !suppressed {
				*issues = append(*issues, model.Issue{
					ID:         fmt.Sprintf("drift:%s:%s", dev.ID, field),
					Severity:   "warning",
					Kind:       "drift",
					EntityKind: "device",
					EntityID:   dev.ID,
					EntityName: dev.Name,
					Field:      field,
					Message:    fmt.Sprintf("Device %q %s drift: manual=%q vs %s=%q", dev.Name, field, manualVal, bestSrc, bestVal),
					DetectedAt: now,
				})
			}
			return manualVal
		}
		if bestVal != "" {
			dev.Provenance = append(dev.Provenance, model.FieldProvenance{
				Field:       field,
				Source:      bestSrc,
				CollectorID: bestCol,
				ObservedAt:  bestAt,
			})
		}
		return bestVal
	}
	pickStr := func(field, manualVal string, precedence []string, getter func(SourceDeviceObs) string) string {
		return pickStrCustom(field, manualVal, precedence, nil, getter, nil)
	}

	// Serial / ServiceTag / SystemUUID
	manualSerial := ""
	if c.Manual != nil {
		manualSerial = c.Manual.Serial
		if manualSerial == "" && c.Manual.Identity != nil {
			manualSerial = c.Manual.Identity.Serial
		}
		if manualSerial == "" && c.Manual.Identity != nil {
			manualSerial = c.Manual.Identity.ServiceTag
		}
		if c.Manual.Identity != nil {
			dev.ServiceTag = c.Manual.Identity.ServiceTag
			dev.SystemUUID = c.Manual.Identity.SystemUUID
		}
	}
	dev.Serial = pickStr("serial", manualSerial, sourcePrecedenceIdentity, func(o SourceDeviceObs) string {
		if s := identity.NormalizeSerial(o.Data.Serial); s != "" {
			return s
		}
		return identity.NormalizeSerial(o.Data.ServiceTag)
	})
	if dev.ServiceTag == "" {
		dev.ServiceTag = pickStr("service_tag", "", sourcePrecedenceIdentity, func(o SourceDeviceObs) string {
			return identity.NormalizeSerial(o.Data.ServiceTag)
		})
	}
	if dev.SystemUUID == "" {
		dev.SystemUUID = pickStr("system_uuid", "", sourcePrecedenceIdentity, func(o SourceDeviceObs) string {
			return identity.NormalizeUUID(o.Data.SystemUUID)
		})
	}

	// Role
	manualRole := ""
	if c.Manual != nil {
		manualRole = c.Manual.Role
	}
	rolePrecedence := []string{"manual", "kubernetes", "snmp", "unifi", "hostos", "redfish", "ipmi", "discovery"}
	dev.Role = pickStrCustom("role", manualRole, rolePrecedence,
		func(o SourceDeviceObs) bool {
			return !(o.Source == "unifi" && o.Data.IsClient)
		},
		func(o SourceDeviceObs) string {
			return o.Data.RoleHint
		},
		func(manVal, liveVal, liveSrc string, liveIsClient bool) bool {
			if liveIsClient && strings.EqualFold(liveVal, "workstation") {
				return true
			}
			if strings.EqualFold(liveVal, "server") && isComputeRole(manVal) {
				return true
			}
			return false
		},
	)
	if dev.Role == "" {
		dev.Role = inferRole(c.Obs)
	}
	dev.RoleColor = roleColors[dev.Role]
	if dev.RoleColor == "" {
		dev.RoleColor = "#64748b"
	}

	// Status (prefer live active/offline when manual isn't explicitly locking e.g. planned/decommissioning)
	manualStatus := ""
	if c.Manual != nil {
		manualStatus = c.Manual.Status
	}
	liveStatus := ""
	for _, src := range []string{"unifi", "kubernetes", "redfish", "snmp", "ipmi", "hostos", "discovery", "lldp"} {
		for _, o := range c.Obs {
			if o.Source == src && o.Data.Status != "" {
				liveStatus = o.Data.Status
				break
			}
		}
		if liveStatus != "" {
			break
		}
	}
	if manualStatus != "" && manualStatus != "active" {
		dev.Status = manualStatus
	} else if liveStatus != "" {
		dev.Status = liveStatus
		dev.Provenance = append(dev.Provenance, model.FieldProvenance{Field: "status", Source: "live"})
	} else if manualStatus != "" {
		dev.Status = manualStatus
	} else {
		dev.Status = "active"
	}

	// Manufacturer & Model & DeviceType
	manualDT := ""
	manualMfg := ""
	manualModel := ""
	manualPartNum := ""
	if c.Manual != nil && c.Manual.DeviceType != "" {
		manualDT = c.Manual.DeviceType
		if dt, ok := dtMap[manualDT]; ok {
			manualMfg = dt.Manufacturer
			manualModel = dt.Model
			manualPartNum = dt.PartNumber
		}
	}
	dev.Manufacturer = pickStr("manufacturer", manualMfg, sourcePrecedenceIdentity, func(o SourceDeviceObs) string {
		return o.Data.Manufacturer
	})
	if dev.Manufacturer == "" {
		dev.Manufacturer = inferManufacturer(c.Obs)
	}
	dev.Model = pickStrCustom("model", manualModel, sourcePrecedenceIdentity, nil,
		func(o SourceDeviceObs) string {
			return o.Data.Model
		},
		func(manVal, liveVal, _ string, _ bool) bool {
			if manualDT != "" && strings.EqualFold(liveVal, manualDT) {
				return true
			}
			if manualPartNum != "" && strings.EqualFold(liveVal, manualPartNum) {
				return true
			}
			nMan, nLive := identity.NormalizeSoftName(manVal), identity.NormalizeSoftName(liveVal)
			return nMan != "" && nMan == nLive
		},
	)
	if manualDT != "" {
		dev.DeviceType = manualDT
	} else {
		// Try matching an existing manual DeviceType by manufacturer + model or model_code.
		dev.DeviceType = matchDeviceType(dev.Manufacturer, dev.Model, c.Obs, dtMap)
	}

	// Platform, OS, Kernel, Firmware, PowerState, Description
	manualPlatform := ""
	manualDesc := ""
	if c.Manual != nil {
		manualPlatform = c.Manual.Platform
		manualDesc = c.Manual.Description
	}
	dev.Platform = pickStr("platform", manualPlatform, sourcePrecedenceIdentity, func(o SourceDeviceObs) string { return o.Data.Platform })
	dev.OSVersion = pickStr("os_version", "", []string{"hostos", "kubernetes", "unifi"}, func(o SourceDeviceObs) string { return o.Data.OSVersion })
	dev.KernelVersion = pickStr("kernel_version", "", []string{"hostos", "kubernetes"}, func(o SourceDeviceObs) string { return o.Data.KernelVersion })
	dev.FirmwareVersion = pickStr("firmware_version", "", []string{"redfish", "unifi", "snmp", "ipmi"}, func(o SourceDeviceObs) string { return o.Data.FirmwareVersion })
	dev.PowerState = pickStr("power_state", "", []string{"redfish", "ipmi"}, func(o SourceDeviceObs) string { return o.Data.PowerState })
	dev.Description = pickStrCustom("description", manualDesc, sourcePrecedenceIdentity, nil,
		func(o SourceDeviceObs) string { return o.Data.Description },
		func(_, _, _ string, _ bool) bool { return true },
	)

	manualOOB := ""
	if c.Manual != nil {
		manualOOB = c.Manual.OOBIP
	}
	dev.OOBIP = pickStr("oob_ip", manualOOB, []string{"manual", "redfish", "ipmi"}, func(o SourceDeviceObs) string {
		return identity.NormalizeIP(o.Data.OOBIP)
	})

	// Interfaces & IPs
	dev.Interfaces = mergeInterfaces(dev.ID, dev.Name, dev.Platform, c, issues, now)

	// Determine PrimaryIP (prefer non-MgmtOnly / non-OOB data-plane IP first).
	dev.PrimaryIP = selectPrimaryIP(dev.Interfaces, dev.OOBIP, c.Obs)

	// Modules & ModuleBays
	dev.ModuleBays, dev.Modules = mergeModules(dev.ID, c, mtMap)

	dev.Sources = sortedKeys(sources)
	dev.CollectorIDs = sortedKeys(collectors)

	// Synthesize DeviceType with parametric faceplate if absent
	if dev.DeviceType == "" || func() bool { _, ok := dtMap[dev.DeviceType]; return !ok }() {
		synDT := synthesizeDeviceType(dev)
		dev.DeviceType = synDT.ID
		if _, exists := dtMap[synDT.ID]; !exists {
			dtMap[synDT.ID] = synDT
		}
	}

	dev.ManualYAMLSnippet = buildManualYAMLSnippet(dev)
	return dev
}

type ifaceBuilder struct {
	model      model.Interface
	manualSpec *manual.InterfaceSpec
	ips        map[string]model.IPAddress
	sources    map[string]struct{}
}

func mergeInterfaces(
	devID, devName, platform string,
	c identityCluster,
	issues *[]model.Issue,
	now time.Time,
) []model.Interface {
	byName := make(map[string]*ifaceBuilder)
	byMAC := make(map[string]*ifaceBuilder)

	ensureBuilder := func(name string) *ifaceBuilder {
		if b, ok := byName[name]; ok {
			return b
		}
		b := &ifaceBuilder{
			model: model.Interface{
				ID:      devID + ":" + name,
				Name:    name,
				Enabled: true,
			},
			ips:     make(map[string]model.IPAddress),
			sources: make(map[string]struct{}),
		}
		byName[name] = b
		return b
	}

	// 1. Seed manual interfaces first
	if c.Manual != nil {
		for _, mi := range c.Manual.Interfaces {
			ms := mi
			b := ensureBuilder(mi.Name)
			b.manualSpec = &ms
			b.sources["manual"] = struct{}{}
			b.model.Type = mi.Type
			b.model.Kind = mi.Kind
			if mi.Enabled != nil {
				b.model.Enabled = *mi.Enabled
			}
			b.model.MgmtOnly = mi.MgmtOnly
			b.model.SpeedMbps = mi.SpeedMbps
			b.model.MTU = mi.MTU
			b.model.Description = mi.Description
			b.model.Mode = mi.Mode
			b.model.UntaggedVLAN = mi.UntaggedVLAN
			b.model.TaggedVLANs = append([]int(nil), mi.TaggedVLANs...)
			if mi.MACAddress != "" {
				if norm, err := identity.NormalizeMAC(mi.MACAddress); err == nil {
					b.model.MACAddress = norm
					byMAC[norm] = b
				}
			}
			for _, ip := range mi.IPAddresses {
				cidr := identity.NormalizeCIDR(ip.Address)
				ipOnly := identity.NormalizeIP(cidr)
				status := ip.Status
				if status == "" {
					status = "active"
				}
				b.ips[cidr] = model.IPAddress{
					ID:          devID + ":" + mi.Name + ":" + cidr,
					Address:     cidr,
					IP:          ipOnly,
					DNSName:     ip.DNSName,
					Description: ip.Description,
					Role:        ip.Role,
					Status:      status,
					Primary:     ip.Primary,
					DeviceID:    devID,
					DeviceName:  devName,
					Interface:   mi.Name,
					Sources:     []string{"manual"},
				}
			}
		}
	}

	// 2. Apply observations in source precedence order
	for _, src := range sourcePrecedencePort {
		if src == "manual" {
			continue
		}
		for _, o := range c.Obs {
			if o.Source != src {
				continue
			}
			for _, oi := range o.Data.Interfaces {
				target := findMatchingInterface(oi, platform, byName, byMAC)
				if target == nil {
					name := strings.TrimSpace(oi.Name)
					if name == "" && len(oi.Candidates) > 0 {
						name = oi.Candidates[0]
					}
					if name == "" {
						continue
					}
					target = ensureBuilder(name)
				}
				target.sources[o.Source] = struct{}{}

				// Drift check on untagged_vlan if manual specified it
				if target.manualSpec != nil && target.manualSpec.UntaggedVLAN != nil && oi.UntaggedVLAN > 0 && *target.manualSpec.UntaggedVLAN != oi.UntaggedVLAN {
					*issues = append(*issues, model.Issue{
						ID:         fmt.Sprintf("drift:%s:%s:vlan", devID, target.model.Name),
						Severity:   "warning",
						Kind:       "drift",
						EntityKind: "device",
						EntityID:   devID,
						EntityName: devName,
						Field:      fmt.Sprintf("interfaces[%s].untagged_vlan", target.model.Name),
						Message:    fmt.Sprintf("Device %q %s untagged VLAN drift: manual=%d vs %s=%d", devName, target.model.Name, *target.manualSpec.UntaggedVLAN, o.Source, oi.UntaggedVLAN),
						DetectedAt: now,
					})
				}

				if target.model.Type == "" && oi.Type != "" {
					target.model.Type = oi.Type
				}
				if target.model.Kind == "" && oi.Kind != "" {
					target.model.Kind = oi.Kind
				}
				if target.manualSpec == nil || target.manualSpec.Enabled == nil {
					if oi.Enabled != nil {
						target.model.Enabled = *oi.Enabled
					}
				}
				if oi.LinkUp != nil && target.model.LinkUp == nil {
					lu := *oi.LinkUp
					target.model.LinkUp = &lu
				}
				if oi.MgmtOnly && !target.model.MgmtOnly {
					target.model.MgmtOnly = true
				}
				if target.model.SpeedMbps == 0 && oi.SpeedMbps > 0 {
					target.model.SpeedMbps = oi.SpeedMbps
				}
				if target.model.MTU == 0 && oi.MTU > 0 {
					target.model.MTU = oi.MTU
				}
				if target.model.MACAddress == "" && oi.MAC != "" {
					if norm, err := identity.NormalizeMAC(oi.MAC); err == nil {
						target.model.MACAddress = norm
						if !identity.IsInvalidMAC(norm) {
							byMAC[norm] = target
						}
					}
				}
				if target.model.Description == "" && oi.Description != "" {
					target.model.Description = oi.Description
				}
				if target.model.PoEMode == "" && oi.PoEMode != "" {
					target.model.PoEMode = oi.PoEMode
				}
				if target.model.PoEWatts == 0 && oi.PoEWatts > 0 {
					target.model.PoEWatts = oi.PoEWatts
				}
				if target.manualSpec == nil || (target.manualSpec.Mode == "" && target.manualSpec.UntaggedVLAN == nil && len(target.manualSpec.TaggedVLANs) == 0) {
					if target.model.Mode == "" && oi.Mode != "" {
						target.model.Mode = oi.Mode
					}
					if target.model.UntaggedVLAN == nil && oi.UntaggedVLAN > 0 {
						v := oi.UntaggedVLAN
						target.model.UntaggedVLAN = &v
					}
					if len(oi.TaggedVLANs) > 0 {
						target.model.TaggedVLANs = unionInts(target.model.TaggedVLANs, oi.TaggedVLANs)
					}
				}

				for _, ip := range oi.IPAddresses {
					cidr := identity.NormalizeCIDR(ip.Address)
					ipOnly := identity.NormalizeIP(cidr)
					if ipOnly == "" {
						continue
					}
					// Match by exact CIDR or same IP with host-mask upgrade
					if cur, ok := target.ips[cidr]; ok {
						cur.Sources = addUnique(cur.Sources, o.Source)
						if cur.DNSName == "" {
							cur.DNSName = ip.DNSName
						}
						if cur.Description == "" {
							cur.Description = ip.Description
						}
						if !cur.Primary && ip.Primary {
							cur.Primary = true
						}
						target.ips[cidr] = cur
					} else {
						target.ips[cidr] = model.IPAddress{
							ID:          devID + ":" + target.model.Name + ":" + cidr,
							Address:     cidr,
							IP:          ipOnly,
							DNSName:     ip.DNSName,
							Description: ip.Description,
							Role:        ip.Role,
							Status:      "active",
							Primary:     ip.Primary,
							DeviceID:    devID,
							DeviceName:  devName,
							Interface:   target.model.Name,
							Sources:     []string{o.Source},
						}
					}
				}
			}
		}
	}

	out := make([]model.Interface, 0, len(byName))
	for _, b := range byName {
		for _, ip := range b.ips {
			b.model.IPAddresses = append(b.model.IPAddresses, ip)
		}
		sort.Slice(b.model.IPAddresses, func(i, j int) bool {
			return b.model.IPAddresses[i].Address < b.model.IPAddresses[j].Address
		})
		b.model.Sources = sortedKeys(b.sources)
		if b.model.Type == "" {
			b.model.Type = "1000base-t"
		}
		out = append(out, b.model)
	}
	sort.Slice(out, func(i, j int) bool {
		ni, nj := identity.PortSortNumber(out[i].Name), identity.PortSortNumber(out[j].Name)
		if ni != nj && ni > 0 && nj > 0 {
			return ni < nj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func mergeModules(devID string, c identityCluster, mtMap map[string]model.ModuleType) ([]model.ModuleBay, []model.DeviceModule) {
	bayMap := make(map[string]model.ModuleBay)
	modMap := make(map[string]model.DeviceModule)

	if c.Manual != nil {
		for _, mb := range c.Manual.ModuleBays {
			bayMap[mb.Name] = model.ModuleBay{
				Name:        mb.Name,
				Position:    mb.Position,
				BayType:     mb.BayType,
				Description: mb.Description,
				Sources:     []string{"manual"},
			}
		}
		for _, m := range c.Manual.Modules {
			status := m.Status
			if status == "" {
				status = "active"
			}
			modMap[m.Bay] = model.DeviceModule{
				ID:          devID + ":" + identity.Slugify(m.Bay),
				Bay:         m.Bay,
				ModuleType:  m.ModuleType,
				Status:      status,
				Serial:      m.Serial,
				AssetTag:    m.AssetTag,
				Description: m.Description,
				Sources:     []string{"manual"},
			}
		}
	}

	// Precedence: redfish > hostos > kubernetes > ipmi
	for _, src := range []string{"redfish", "hostos", "kubernetes", "ipmi", "snmp"} {
		for _, o := range c.Obs {
			if o.Source != src {
				continue
			}
			for idx, om := range o.Data.Modules {
				bayName := strings.TrimSpace(om.Bay)
				if bayName == "" {
					bayName = fmt.Sprintf("%s %d", om.Profile, idx+1)
				}
				if b, ok := bayMap[bayName]; ok {
					b.Sources = addUnique(b.Sources, o.Source)
					bayMap[bayName] = b
				} else {
					bayMap[bayName] = model.ModuleBay{
						Name:        bayName,
						Position:    strconv.Itoa(len(bayMap) + 1),
						BayType:     om.BayType,
						Description: om.BayDesc,
						Sources:     []string{o.Source},
					}
				}

				mfg := strings.TrimSpace(om.Manufacturer)
				if mfg == "" {
					mfg = "Generic"
				}
				mdl := strings.TrimSpace(om.Model)
				if mdl == "" {
					mdl = om.Profile
				}
				mtID := identity.Slugify(mfg + "-" + mdl)
				if _, exists := mtMap[mtID]; !exists {
					mtMap[mtID] = model.ModuleType{
						ID:           mtID,
						Manufacturer: mfg,
						Model:        mdl,
						Profile:      om.Profile,
						BayType:      om.BayType,
						PartNumber:   om.PartNumber,
						Description:  om.Description,
						Attributes:   om.Attributes,
						Sources:      []string{o.Source},
					}
				}

				if cur, ok := modMap[bayName]; ok {
					cur.Sources = addUnique(cur.Sources, o.Source)
					if cur.Serial == "" {
						cur.Serial = identity.NormalizeSerial(om.Serial)
					}
					if cur.Manufacturer == "" {
						cur.Manufacturer = mfg
					}
					if cur.Model == "" {
						cur.Model = mdl
					}
					if cur.Profile == "" {
						cur.Profile = om.Profile
					}
					modMap[bayName] = cur
				} else {
					status := om.Status
					if status == "" {
						status = "active"
					}
					modMap[bayName] = model.DeviceModule{
						ID:           devID + ":" + identity.Slugify(bayName),
						Bay:          bayName,
						ModuleType:   mtID,
						Profile:      om.Profile,
						Manufacturer: mfg,
						Model:        mdl,
						Status:       status,
						Serial:       identity.NormalizeSerial(om.Serial),
						Description:  om.Description,
						Sources:      []string{o.Source},
					}
				}
			}
		}
	}

	bays := make([]model.ModuleBay, 0, len(bayMap))
	for _, b := range bayMap {
		bays = append(bays, b)
	}
	sort.Slice(bays, func(i, j int) bool { return bays[i].Name < bays[j].Name })

	mods := make([]model.DeviceModule, 0, len(modMap))
	for _, m := range modMap {
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Bay < mods[j].Bay })
	return bays, mods
}

func resolveCables(
	inv *manual.Inventory,
	linkObs []SourceLinkObs,
	devObs []SourceDeviceObs,
	obsToCanonical map[int]string,
	devByID map[string]*model.Device,
	devByName map[string]*model.Device,
) []model.Cable {
	cables := make(map[string]model.Cable)
	endpointBusy := make(map[string]string) // devID:port -> cableKey

	// Build secondary lookups for links (obsKey -> device, mac -> device+iface, ip -> device, softname -> device)
	obsKeyToDev := make(map[string]*model.Device)
	macToEndpoint := make(map[string]struct {
		dev  *model.Device
		port string
	})
	ipToDev := make(map[string]*model.Device)
	softNameToDev := make(map[string]*model.Device)

	for i, o := range devObs {
		cid := obsToCanonical[i]
		dev := devByID[cid]
		if dev == nil {
			continue
		}
		obsKeyToDev[o.CollectorID+"|"+o.Source+"|"+o.ObsKey] = dev
		obsKeyToDev[o.ObsKey] = dev
		if o.Data.PrimaryIP != "" {
			ipToDev[identity.NormalizeIP(o.Data.PrimaryIP)] = dev
		}
		if o.Data.MgmtIP != "" {
			ipToDev[identity.NormalizeIP(o.Data.MgmtIP)] = dev
		}
		for _, m := range o.Data.Claims.MACs {
			if norm, err := identity.NormalizeMAC(m); err == nil {
				if _, exists := macToEndpoint[norm]; !exists {
					macToEndpoint[norm] = struct {
						dev  *model.Device
						port string
					}{dev: dev}
				}
			}
		}
	}
	for _, d := range devByID {
		if soft := identity.NormalizeSoftName(identity.ShortHostname(d.Name)); soft != "" {
			softNameToDev[soft] = d
		}
		if softFull := identity.NormalizeSoftName(d.Name); softFull != "" {
			softNameToDev[softFull] = d
		}
		for _, iface := range d.Interfaces {
			if iface.MACAddress != "" && iface.Kind != "virtual" && iface.Kind != "lag" {
				if norm, err := identity.NormalizeMAC(iface.MACAddress); err == nil {
					macToEndpoint[norm] = struct {
						dev  *model.Device
						port string
					}{dev: d, port: iface.Name}
				}
			}
			for _, ip := range iface.IPAddresses {
				if ip.IP != "" {
					ipToDev[ip.IP] = d
				}
			}
		}
	}

	resolveDev := func(obsKey, devName, mac, ip, chassis, collectorID, source string) *model.Device {
		if obsKey != "" {
			if d, ok := obsKeyToDev[collectorID+"|"+source+"|"+obsKey]; ok {
				return d
			}
			if d, ok := obsKeyToDev[obsKey]; ok {
				return d
			}
		}
		if devName != "" {
			if d, ok := devByName[devName]; ok {
				return d
			}
			if soft := identity.NormalizeSoftName(identity.ShortHostname(devName)); soft != "" {
				if d, ok := softNameToDev[soft]; ok {
					return d
				}
			}
		}
		for _, rawMAC := range []string{mac, chassis} {
			if rawMAC == "" {
				continue
			}
			if norm, err := identity.NormalizeMAC(rawMAC); err == nil {
				if ep, ok := macToEndpoint[norm]; ok {
					return ep.dev
				}
			}
		}
		if ip != "" {
			if d, ok := ipToDev[identity.NormalizeIP(ip)]; ok {
				return d
			}
		}
		return nil
	}

	resolvePort := func(d *model.Device, rawPort, portDescr string, candidates []string, macHint string) string {
		if d == nil {
			return rawPort
		}
		if (rawPort == "" || strings.EqualFold(rawPort, "eth0")) && macHint != "" {
			if norm, err := identity.NormalizeMAC(macHint); err == nil {
				for _, iface := range d.Interfaces {
					if iface.MACAddress == norm && iface.Kind != "virtual" && !isVirtualInterfaceName(iface.Name) {
						return iface.Name
					}
				}
			}
		}
		tryNames := make([]string, 0, len(candidates)+4)
		if rawPort != "" {
			tryNames = append(tryNames, rawPort)
		}
		if portDescr != "" {
			tryNames = append(tryNames, portDescr)
		}
		tryNames = append(tryNames, candidates...)
		if strings.EqualFold(d.Platform, "unifi") {
			if u := identity.UnifiPortCandidate(rawPort); u != "" {
				tryNames = append([]string{u}, tryNames...)
			}
			if u := identity.UnifiPortCandidate(portDescr); u != "" {
				tryNames = append([]string{u}, tryNames...)
			}
		}
		tryNames = append(tryNames, identity.PortCandidates(rawPort, portDescr, "")...)

		for _, cand := range tryNames {
			for _, iface := range d.Interfaces {
				if strings.EqualFold(iface.Name, cand) && iface.Kind != "virtual" {
					return iface.Name
				}
			}
		}
		if macHint != "" {
			if norm, err := identity.NormalizeMAC(macHint); err == nil {
				for _, iface := range d.Interfaces {
					if iface.MACAddress == norm && iface.Kind != "virtual" {
						return iface.Name
					}
				}
			}
		}
		if rawPort != "" {
			return rawPort
		}
		return "Port 1"
	}

	addCable := func(devA, devB *model.Device, portA, portB, cType, status, method, conf, color, label, desc, src string, speed int) {
		if devA == nil || devB == nil || devA.ID == devB.ID || portA == "" || portB == "" {
			return
		}
		key := manual.CanonicalCableKey(devA.ID, portA, devB.ID, portB)
		if existing, ok := cables[key]; ok {
			existing.Sources = addUnique(existing.Sources, src)
			if existing.Method == method && method == "lldp" && existing.ADeviceID != devA.ID {
				// Bidirectional LLDP confirmation from both peers!
				existing.Confidence = "verified"
			}
			cables[key] = existing
			return
		}
		// Avoid attaching two different cables to the exact same physical switch port
		// unless manual overrides.
		epA := devA.ID + ":" + portA
		epB := devB.ID + ":" + portB
		if conf != "manual" {
			if _, busy := endpointBusy[epA]; busy {
				return
			}
			if _, busy := endpointBusy[epB]; busy {
				return
			}
		}
		endpointBusy[epA] = key
		endpointBusy[epB] = key
		if cType == "" {
			cType = "cat6"
		}
		if status == "" {
			status = "connected"
		}
		cables[key] = model.Cable{
			ID:          "cable-" + shortHash(key),
			ADeviceID:   devA.ID,
			ADevice:     devA.Name,
			APort:       portA,
			BDeviceID:   devB.ID,
			BDevice:     devB.Name,
			BPort:       portB,
			Type:        cType,
			Status:      status,
			Method:      method,
			Confidence:  conf,
			SpeedMbps:   speed,
			Color:       color,
			Label:       label,
			Description: desc,
			Sources:     []string{src},
		}
	}

	// Priority 1: Manual cables (and expanded uplinks)
	for _, mc := range inv.Cables {
		devA := devByName[mc.A.Device]
		devB := devByName[mc.B.Device]
		addCable(devA, devB, mc.A.Port, mc.B.Port, mc.Type, mc.Status, "manual", "manual", mc.Color, mc.Label, mc.Description, "manual", 0)
	}

	// Priority 2: LLDP / CDP
	// Priority 3: UniFi Uplinks & Clients
	// Priority 4: SNMP FDB
	methodOrder := []struct {
		methods map[string]bool
		conf    string
	}{
		{map[string]bool{"lldp": true, "cdp": true}, "observed"},
		{map[string]bool{"unifi_uplink": true, "unifi_client": true}, "observed"},
		{map[string]bool{"snmp_fdb": true, "arp": true}, "inferred"},
	}

	for _, tier := range methodOrder {
		for _, lo := range linkObs {
			if !tier.methods[lo.Data.Method] {
				continue
			}
			l := lo.Data
			devA := resolveDev(l.LocalObsKey, l.LocalDeviceName, l.LocalMAC, l.LocalIP, "", lo.CollectorID, lo.Source)
			devB := resolveDev(l.RemoteObsKey, l.RemoteDeviceName, l.RemoteMAC, l.RemoteIP, l.RemoteChassisID, lo.CollectorID, lo.Source)
			if devA == nil || devB == nil {
				continue
			}
			portA := resolvePort(devA, l.LocalPort, "", l.LocalPortCandidates, l.LocalMAC)
			portB := resolvePort(devB, l.RemotePort, l.RemotePortDescr, l.RemotePortCandidates, l.RemoteMAC)
			if l.Method == "snmp_fdb" && strings.Contains(l.Description, "(LAG ") {
				if _, busy := endpointBusy[devB.ID+":"+portB]; busy {
					portB = findFreePhysicalPort(devB, endpointBusy)
				}
			}
			conf := tier.conf
			if l.Verified {
				conf = "verified"
			}
			addCable(devA, devB, portA, portB, l.CableType, "connected", l.Method, conf, "", "", l.Description, lo.Source, l.SpeedMbps)
		}
	}

	out := make([]model.Cable, 0, len(cables))
	for _, cab := range cables {
		out = append(out, cab)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func findFreePhysicalPort(d *model.Device, endpointBusy map[string]string) string {
	for _, iface := range d.Interfaces {
		if iface.MgmtOnly || iface.Kind == "virtual" || iface.Kind == "lag" || isVirtualInterfaceName(iface.Name) {
			continue
		}
		if _, busy := endpointBusy[d.ID+":"+iface.Name]; !busy {
			return iface.Name
		}
	}
	for n := 2; n < 64; n++ {
		cand := fmt.Sprintf("nic%d", n)
		if _, busy := endpointBusy[d.ID+":"+cand]; !busy {
			return cand
		}
	}
	return "nic2"
}

func findMatchingInterface(oi protocol.InterfaceObservation, platform string, byName map[string]*ifaceBuilder, byMAC map[string]*ifaceBuilder) *ifaceBuilder {
	if oi.MAC != "" {
		if norm, err := identity.NormalizeMAC(oi.MAC); err == nil && !identity.IsInvalidMAC(norm) {
			if b, ok := byMAC[norm]; ok {
				return b
			}
		}
	}
	tryNames := make([]string, 0, len(oi.Candidates)+4)
	if oi.Name != "" {
		tryNames = append(tryNames, oi.Name)
	}
	if strings.EqualFold(platform, "unifi") {
		if u := identity.UnifiPortCandidate(oi.Name); u != "" {
			tryNames = append(tryNames, u)
		}
	}
	tryNames = append(tryNames, oi.Candidates...)
	tryNames = append(tryNames, identity.PortCandidates(oi.Name, "", "")...)
	for _, n := range tryNames {
		if b, ok := byName[n]; ok {
			return b
		}
		for k, b := range byName {
			if strings.EqualFold(k, n) {
				return b
			}
		}
	}
	return nil
}

func jsonUnmarshal(s string, dst any) error {
	return json.Unmarshal([]byte(s), dst)
}

func decodeObservations(rawObs []db.StoredObservation) (
	[]SourceDeviceObs,
	[]SourceLinkObs,
	[]SourceVLANObs,
	[]SourcePrefixObs,
) {
	var devs []SourceDeviceObs
	var links []SourceLinkObs
	var vlans []SourceVLANObs
	var prefixes []SourcePrefixObs

	for _, r := range rawObs {
		switch r.Kind {
		case "device":
			var d protocol.DeviceObservation
			if err := jsonUnmarshal(r.PayloadJSON, &d); err == nil {
				devs = append(devs, SourceDeviceObs{
					ObsID: r.ID, CollectorID: r.CollectorID, Site: r.Site, Source: r.Source,
					Profile: r.Profile, ObsKey: r.ObsKey, ObservedAt: r.ObservedAt, Data: d,
				})
			}
		case "link":
			var l protocol.LinkObservation
			if err := jsonUnmarshal(r.PayloadJSON, &l); err == nil {
				links = append(links, SourceLinkObs{
					CollectorID: r.CollectorID, Site: r.Site, Source: r.Source,
					Profile: r.Profile, ObservedAt: r.ObservedAt, Data: l,
				})
			}
		case "vlan":
			var v protocol.VLANObservation
			if err := jsonUnmarshal(r.PayloadJSON, &v); err == nil {
				vlans = append(vlans, SourceVLANObs{CollectorID: r.CollectorID, Source: r.Source, ObservedAt: r.ObservedAt, Data: v})
			}
		case "prefix":
			var p protocol.PrefixObservation
			if err := jsonUnmarshal(r.PayloadJSON, &p); err == nil {
				prefixes = append(prefixes, SourcePrefixObs{CollectorID: r.CollectorID, Source: r.Source, ObservedAt: r.ObservedAt, Data: p})
			}
		}
	}
	return devs, links, vlans, prefixes
}

func matchDeviceType(mfg, mdl string, obs []SourceDeviceObs, dtMap map[string]model.DeviceType) string {
	normMfg := identity.NormalizeSoftName(mfg)
	normMdl := identity.NormalizeSoftName(mdl)
	for id, dt := range dtMap {
		if normMdl != "" && identity.NormalizeSoftName(dt.Model) == normMdl {
			if normMfg == "" || identity.NormalizeSoftName(dt.Manufacturer) == normMfg {
				return id
			}
		}
		for _, o := range obs {
			if o.Data.ModelCode != "" && (strings.EqualFold(dt.PartNumber, o.Data.ModelCode) || strings.EqualFold(id, o.Data.ModelCode)) {
				return id
			}
		}
	}
	return ""
}

func convertFaceplate(spec *manual.FaceplateSpec) *model.FaceplateSpec {
	if spec == nil {
		return nil
	}
	conv := func(in []manual.FaceplateElementSpec) []model.FaceplateElement {
		out := make([]model.FaceplateElement, 0, len(in))
		for _, e := range in {
			out = append(out, model.FaceplateElement{
				Kind: e.Kind, Label: e.Label, Connector: e.Connector, FormFactor: e.FormFactor,
				Rows: e.Rows, Cols: e.Cols, Start: e.Start, Count: e.Count, Width: e.Width,
				Prefix: e.Prefix, Names: append([]string(nil), e.Names...), PoE: e.PoE,
			})
		}
		return out
	}
	return &model.FaceplateSpec{
		Front: conv(spec.Front),
		Rear:  conv(spec.Rear),
	}
}

func buildManualYAMLSnippet(d model.Device) string {
	spec := manual.DeviceSpec{
		Name:       d.Name,
		Site:       d.Site,
		Role:       d.Role,
		DeviceType: d.DeviceType,
		Status:     d.Status,
		Owner:      d.Owner,
		Serial:     d.Serial,
		OOBIP:      d.OOBIP,
	}
	if d.SystemUUID != "" || d.ServiceTag != "" {
		spec.Identity = &manual.IdentitySpec{
			ServiceTag: d.ServiceTag,
			SystemUUID: d.SystemUUID,
		}
	}
	for _, iface := range d.Interfaces {
		if iface.MACAddress != "" && iface.Kind != "virtual" && !isVirtualInterfaceName(iface.Name) {
			if spec.Identity == nil {
				spec.Identity = &manual.IdentitySpec{}
			}
			spec.Identity.MACs = append(spec.Identity.MACs, iface.MACAddress)
			if len(spec.Identity.MACs) >= 2 {
				break
			}
		}
	}
	doc := struct {
		Devices []manual.DeviceSpec `yaml:"devices"`
	}{Devices: []manual.DeviceSpec{spec}}
	b, _ := yaml.Marshal(doc)
	return string(b)
}

func prefixHostCapacity(p netip.Prefix) int {
	if p.Addr().Is6() {
		return 0
	}
	hostBits := 32 - p.Bits()
	if hostBits <= 0 {
		return 1
	}
	if hostBits == 1 {
		return 2
	}
	if hostBits >= 31 {
		return 0
	}
	return (1 << hostBits) - 2
}

func addUnique(slice []string, item string) []string {
	if item == "" {
		return slice
	}
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	out := append(slice, item)
	sort.Strings(out)
	return out
}

func unionInts(a, b []int) []int {
	set := make(map[int]struct{}, len(a)+len(b))
	for _, v := range a {
		set[v] = struct{}{}
	}
	for _, v := range b {
		set[v] = struct{}{}
	}
	out := make([]int, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func firstSourceOrManual(c identityCluster) string {
	if c.Manual != nil {
		return "manual"
	}
	if len(c.Obs) > 0 {
		return c.Obs[0].Source
	}
	return "auto"
}

func isComputeRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "server", "control-plane", "worker", "storage", "hypervisor":
		return true
	}
	return false
}

func selectPrimaryIP(ifaces []model.Interface, oobIP string, obs []SourceDeviceObs) string {
	sourceRank := func(srcs []string) int {
		order := []string{"manual", "kubernetes", "hostos", "unifi", "snmp", "lldp", "redfish", "ipmi", "discovery"}
		best := 99
		for _, s := range srcs {
			for idx, p := range order {
				if s == p && idx < best {
					best = idx
				}
			}
		}
		return best
	}

	// Pass (a): ip.Primary on non-MgmtOnly interface where ip != oobIP
	bestIP := ""
	bestRank := 999
	for _, iface := range ifaces {
		if iface.MgmtOnly {
			continue
		}
		for _, ip := range iface.IPAddresses {
			if ip.Primary && ip.IP != "" && ip.IP != oobIP {
				if r := sourceRank(ip.Sources); r < bestRank {
					bestRank = r
					bestIP = ip.IP
				}
			}
		}
	}
	if bestIP != "" {
		return bestIP
	}

	// Pass (b): any IP on non-MgmtOnly interface where ip != oobIP
	bestRank = 999
	for _, iface := range ifaces {
		if iface.MgmtOnly {
			continue
		}
		for _, ip := range iface.IPAddresses {
			if ip.IP != "" && ip.IP != oobIP {
				if r := sourceRank(ip.Sources); r < bestRank {
					bestRank = r
					bestIP = ip.IP
				}
			}
		}
	}
	if bestIP != "" {
		return bestIP
	}

	// Pass (c): observation PrimaryIP where ip != oobIP
	for _, src := range []string{"kubernetes", "hostos", "unifi", "snmp", "discovery", "lldp", "redfish", "ipmi"} {
		for _, o := range obs {
			if o.Source != src {
				continue
			}
			if ip := identity.NormalizeIP(o.Data.PrimaryIP); ip != "" && ip != oobIP {
				return ip
			}
		}
	}

	// Pass (d): fallback to MgmtOnly primary -> any interface IP -> oobIP -> MgmtIP
	for _, iface := range ifaces {
		for _, ip := range iface.IPAddresses {
			if ip.Primary && ip.IP != "" {
				return ip.IP
			}
		}
	}
	for _, iface := range ifaces {
		for _, ip := range iface.IPAddresses {
			if ip.IP != "" {
				return ip.IP
			}
		}
	}
	if oobIP != "" {
		return oobIP
	}
	for _, o := range obs {
		if ip := identity.NormalizeIP(o.Data.MgmtIP); ip != "" {
			return ip
		}
	}
	return ""
}

func sortState(state *model.CanonicalState) {
	sort.Slice(state.Sites, func(i, j int) bool { return state.Sites[i].ID < state.Sites[j].ID })
	sort.Slice(state.Locations, func(i, j int) bool { return state.Locations[i].ID < state.Locations[j].ID })
	sort.Slice(state.OwnerGroups, func(i, j int) bool { return state.OwnerGroups[i].Name < state.OwnerGroups[j].Name })
	sort.Slice(state.Owners, func(i, j int) bool { return state.Owners[i].Name < state.Owners[j].Name })
	sort.Slice(state.Contacts, func(i, j int) bool { return state.Contacts[i].Name < state.Contacts[j].Name })
	sort.Slice(state.DeviceTypes, func(i, j int) bool { return state.DeviceTypes[i].ID < state.DeviceTypes[j].ID })
	sort.Slice(state.ModuleTypes, func(i, j int) bool { return state.ModuleTypes[i].ID < state.ModuleTypes[j].ID })
	sort.Slice(state.Racks, func(i, j int) bool { return state.Racks[i].ID < state.Racks[j].ID })
	sort.Slice(state.Devices, func(i, j int) bool { return state.Devices[i].Name < state.Devices[j].Name })
	sort.Slice(state.VLANs, func(i, j int) bool {
		if state.VLANs[i].Site != state.VLANs[j].Site {
			return state.VLANs[i].Site < state.VLANs[j].Site
		}
		return state.VLANs[i].VID < state.VLANs[j].VID
	})
	sort.Slice(state.Prefixes, func(i, j int) bool { return state.Prefixes[i].CIDR < state.Prefixes[j].CIDR })
	sort.Slice(state.Cables, func(i, j int) bool { return state.Cables[i].ID < state.Cables[j].ID })
	sort.Slice(state.Assets, func(i, j int) bool { return state.Assets[i].Name < state.Assets[j].Name })
}
