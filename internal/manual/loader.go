package manual

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/model"
	"gopkg.in/yaml.v3"
)

var ignoredFilenames = map[string]struct{}{
	"kustomization.yaml": {},
	"kustomization.yml":  {},
	"collector.yaml":     {},
	"collector.yml":      {},
	"settings.yaml":      {},
	"settings.yml":       {},
}

// DiscoverFiles returns sorted deduplicated YAML files under root, ignoring dotfiles
// and Kubernetes ConfigMap internal directories (such as ..data).
func DiscoverFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("manual path %q is not a directory", root)
	}

	seen := make(map[string]struct{})
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if strings.HasPrefix(part, ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}
		if _, skip := ignoredFilenames[name]; skip {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			resolved = path
		}
		if _, ok := seen[resolved]; ok {
			return nil
		}
		seen[resolved] = struct{}{}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// LoadDir parses and merges all manual YAML files from dir.
// Individual parse/validation problems are surfaced in Inventory.Issues rather than failing hard when possible.
func LoadDir(dir string) (*Inventory, error) {
	now := time.Now().UTC()
	inv := &Inventory{
		Dir:       dir,
		RackPlans: make(map[string]RackPlan),
		Rules: EffectiveRules{
			AutoNetworkDevices:  true,
			AutoServers:         true,
			AutoWiredClients:    true,
			AutoWirelessClients: false,
		},
	}

	if dir == "" {
		return inv, nil
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		inv.Issues = append(inv.Issues, model.Issue{
			ID:         "manual-dir-missing",
			Severity:   "warning",
			Kind:       "yaml_validation",
			Message:    fmt.Sprintf("Manual YAML directory %q does not exist; running in pure auto-discovery mode.", dir),
			FilePath:   dir,
			DetectedAt: now,
		})
		return inv, nil
	}

	files, err := DiscoverFiles(dir)
	if err != nil {
		return nil, err
	}
	inv.FilesLoaded = files

	hasher := sha256.New()
	var docs []struct {
		file string
		doc  Document
	}

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			inv.Issues = append(inv.Issues, model.Issue{
				ID:         "yaml-read:" + file,
				Severity:   "error",
				Kind:       "yaml_validation",
				Message:    fmt.Sprintf("Failed to read YAML file: %v", err),
				FilePath:   file,
				DetectedAt: now,
			})
			continue
		}
		hasher.Write([]byte(file))
		hasher.Write(raw)

		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var doc Document
			err := dec.Decode(&doc)
			if err == io.EOF {
				break
			}
			if err != nil {
				inv.Issues = append(inv.Issues, model.Issue{
					ID:         "yaml-parse:" + file,
					Severity:   "error",
					Kind:       "yaml_validation",
					Message:    fmt.Sprintf("YAML syntax error in %s: %v", filepath.Base(file), err),
					FilePath:   file,
					DetectedAt: now,
				})
				break
			}
			docs = append(docs, struct {
				file string
				doc  Document
			}{file: file, doc: doc})
		}
	}
	inv.Hash = hex.EncodeToString(hasher.Sum(nil))

	for _, entry := range docs {
		if entry.doc.Defaults != nil {
			mergeDefaults(&inv.Defaults, *entry.doc.Defaults)
		}
		if entry.doc.Rules != nil {
			mergeRules(&inv.Rules, *entry.doc.Rules)
		}
	}

	siteSeen := make(map[string]int)
	locSeen := make(map[string]int)
	ogSeen := make(map[string]int)
	ownerSeen := make(map[string]int)
	contactSeen := make(map[string]int)
	dtSeen := make(map[string]int)
	mtSeen := make(map[string]int)
	rackSeen := make(map[string]int)
	vlanSeen := make(map[string]int)
	pfxSeen := make(map[string]int)
	devSeen := make(map[string]int)
	cableSeen := make(map[string]int)
	assetSeen := make(map[string]int)

	for _, entry := range docs {
		d := entry.doc
		file := entry.file

		for _, item := range d.Sites {
			if item.ID == "" {
				item.ID = identity.Slugify(item.Name)
			}
			if item.Status == "" {
				item.Status = "active"
			}
			if item.ID == "" {
				continue
			}
			if _, dup := siteSeen[item.ID]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("site", item.ID, file, now))
				continue
			}
			siteSeen[item.ID] = len(inv.Sites)
			inv.Sites = append(inv.Sites, item)
		}

		for _, item := range d.Locations {
			if item.Site == "" {
				item.Site = inv.Defaults.Site
			}
			if item.ID == "" {
				item.ID = identity.Slugify(item.Name)
			}
			key := item.Site + "/" + item.ID
			if _, dup := locSeen[key]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("location", key, file, now))
				continue
			}
			locSeen[key] = len(inv.Locations)
			inv.Locations = append(inv.Locations, item)
		}

		for _, item := range d.OwnerGroups {
			if item.Name == "" {
				continue
			}
			if _, dup := ogSeen[item.Name]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("owner_group", item.Name, file, now))
				continue
			}
			ogSeen[item.Name] = len(inv.OwnerGroups)
			inv.OwnerGroups = append(inv.OwnerGroups, item)
		}

		for _, item := range d.Owners {
			if item.Name == "" {
				continue
			}
			if _, dup := ownerSeen[item.Name]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("owner", item.Name, file, now))
				continue
			}
			ownerSeen[item.Name] = len(inv.Owners)
			inv.Owners = append(inv.Owners, item)
		}

		for _, item := range d.Contacts {
			if item.Name == "" {
				continue
			}
			key := item.Name + "|" + item.Role + "|" + item.Owner
			if _, dup := contactSeen[key]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("contact", item.Name, file, now))
				continue
			}
			contactSeen[key] = len(inv.Contacts)
			inv.Contacts = append(inv.Contacts, item)
		}

		for _, item := range d.DeviceTypes {
			if item.ID == "" {
				item.ID = identity.Slugify(item.Manufacturer + "-" + item.Model)
			}
			if item.UHeight <= 0 {
				item.UHeight = 1
			}
			if item.Depth == "" {
				item.Depth = "full"
			}
			if _, dup := dtSeen[item.ID]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("device_type", item.ID, file, now))
				continue
			}
			dtSeen[item.ID] = len(inv.DeviceTypes)
			inv.DeviceTypes = append(inv.DeviceTypes, item)
		}

		for _, item := range d.ModuleTypes {
			if item.ID == "" {
				item.ID = identity.Slugify(item.Manufacturer + "-" + item.Model)
			}
			if _, dup := mtSeen[item.ID]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("module_type", item.ID, file, now))
				continue
			}
			mtSeen[item.ID] = len(inv.ModuleTypes)
			inv.ModuleTypes = append(inv.ModuleTypes, item)
		}

		for _, item := range d.Racks {
			if item.Site == "" {
				item.Site = inv.Defaults.Site
			}
			if item.Status == "" {
				item.Status = "active"
			}
			if item.Width <= 0 {
				item.Width = 19
			}
			if item.UHeight <= 0 {
				item.UHeight = 42
			}
			key := item.Site + "/" + item.Name
			if _, dup := rackSeen[key]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("rack", key, file, now))
				continue
			}
			rackSeen[key] = len(inv.Racks)
			inv.Racks = append(inv.Racks, item)
		}

		for _, item := range d.VLANs {
			if item.Site == "" {
				item.Site = inv.Defaults.Site
			}
			if item.Status == "" {
				item.Status = "active"
			}
			key := item.Site + ":" + strconv.Itoa(item.VID)
			if _, dup := vlanSeen[key]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("vlan", key, file, now))
				continue
			}
			vlanSeen[key] = len(inv.VLANs)
			inv.VLANs = append(inv.VLANs, item)
		}

		for _, item := range d.Prefixes {
			if item.Site == "" {
				item.Site = inv.Defaults.Site
			}
			if item.Status == "" {
				item.Status = "active"
			}
			item.CIDR = identity.NormalizePrefix(item.CIDR)
			if _, dup := pfxSeen[item.CIDR]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("prefix", item.CIDR, file, now))
				continue
			}
			pfxSeen[item.CIDR] = len(inv.Prefixes)
			inv.Prefixes = append(inv.Prefixes, item)
		}

		for _, item := range d.Devices {
			if item.Name == "" {
				continue
			}
			applyDeviceDefaults(&item, inv.Defaults)
			if _, dup := devSeen[item.Name]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("device", item.Name, file, now))
				continue
			}
			devSeen[item.Name] = len(inv.Devices)
			inv.Devices = append(inv.Devices, item)
		}

		for _, item := range d.Cables {
			if item.A.Device == "" || item.B.Device == "" {
				continue
			}
			if item.Status == "" {
				item.Status = "connected"
			}
			if item.Type == "" {
				item.Type = "cat6"
			}
			key := CanonicalCableKey(item.A.Device, item.A.Port, item.B.Device, item.B.Port)
			if _, dup := cableSeen[key]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("cable", key, file, now))
				continue
			}
			cableSeen[key] = len(inv.Cables)
			inv.Cables = append(inv.Cables, item)
		}

		for _, item := range d.Assets {
			if item.Site == "" {
				item.Site = inv.Defaults.Site
			}
			if item.Owner == "" {
				item.Owner = inv.Defaults.Owner
			}
			if item.Status == "" {
				item.Status = "active"
			}
			key := item.AssetTag
			if key == "" {
				key = item.Name
			}
			if _, dup := assetSeen[key]; dup {
				inv.Issues = append(inv.Issues, duplicateIssue("asset", key, file, now))
				continue
			}
			assetSeen[key] = len(inv.Assets)
			inv.Assets = append(inv.Assets, item)
		}
	}

	// Expand device uplinks shorthand into cables.
	for _, dev := range inv.Devices {
		for _, up := range dev.Uplinks {
			if up.Switch == "" || up.Port == "" {
				continue
			}
			localPort := up.Interface
			if localPort == "" {
				localPort = "Port 1"
			}
			remotePort := up.Port
			if _, err := strconv.Atoi(remotePort); err == nil {
				remotePort = "Port " + remotePort
			}
			cableType := up.Type
			if cableType == "" {
				cableType = "cat6"
			}
			cable := CableSpec{
				A:      CableEndpointSpec{Device: dev.Name, Port: localPort},
				B:      CableEndpointSpec{Device: up.Switch, Port: remotePort},
				Type:   cableType,
				Status: "connected",
				Color:  up.Color,
			}
			key := CanonicalCableKey(cable.A.Device, cable.A.Port, cable.B.Device, cable.B.Port)
			if _, exists := cableSeen[key]; !exists {
				cableSeen[key] = len(inv.Cables)
				inv.Cables = append(inv.Cables, cable)
			}
		}
	}

	// Compute rack layouts and attach computed positions to devices.
	devMap := make(map[string]DeviceSpec, len(inv.Devices))
	for _, dev := range inv.Devices {
		devMap[dev.Name] = dev
	}
	dtMap := make(map[string]DeviceTypeSpec, len(inv.DeviceTypes))
	for _, dt := range inv.DeviceTypes {
		dtMap[dt.ID] = dt
	}
	for _, rack := range inv.Racks {
		plan := PlanRackLayout(rack, devMap, dtMap)
		rackKey := rack.Site + "/" + rack.Name
		inv.RackPlans[rackKey] = plan
		inv.Issues = append(inv.Issues, plan.Issues...)
		for devName, placement := range plan.Positions {
			if idx, ok := devSeen[devName]; ok {
				pos := placement.StartU
				inv.Devices[idx].Rack = rack.Name
				inv.Devices[idx].Position = &pos
				inv.Devices[idx].Face = placement.Face
			}
		}
	}

	return inv, nil
}

// CanonicalCableKey creates an order-independent key for a cable connecting two endpoints.
func CanonicalCableKey(devA, portA, devB, portB string) string {
	left := devA + ":" + portA
	right := devB + ":" + portB
	if left <= right {
		return left + "<->" + right
	}
	return right + "<->" + left
}

func mergeDefaults(dst *Defaults, src Defaults) {
	if src.Site != "" {
		dst.Site = src.Site
	}
	if src.Location != "" {
		dst.Location = src.Location
	}
	if src.Owner != "" {
		dst.Owner = src.Owner
	}
	if src.Rack != "" {
		dst.Rack = src.Rack
	}
	if src.Status != "" {
		dst.Status = src.Status
	}
	if src.ComputeRole != "" {
		dst.ComputeRole = src.ComputeRole
	}
	dst.Tags = unionStrings(dst.Tags, src.Tags)
}

func mergeRules(dst *EffectiveRules, src RulesSpec) {
	if src.AutoCreate != nil {
		if src.AutoCreate.NetworkDevices != nil {
			dst.AutoNetworkDevices = *src.AutoCreate.NetworkDevices
		}
		if src.AutoCreate.Servers != nil {
			dst.AutoServers = *src.AutoCreate.Servers
		}
		if src.AutoCreate.WiredClients != nil {
			dst.AutoWiredClients = *src.AutoCreate.WiredClients
		}
		if src.AutoCreate.WirelessClients != nil {
			dst.AutoWirelessClients = *src.AutoCreate.WirelessClients
		}
	}
	if src.Ignore != nil {
		dst.IgnoreCIDRs = unionStrings(dst.IgnoreCIDRs, src.Ignore.CIDRs)
		dst.IgnoreMACPrefixes = unionStrings(dst.IgnoreMACPrefixes, src.Ignore.MACPrefixes)
		dst.IgnoreHostnames = unionStrings(dst.IgnoreHostnames, src.Ignore.Hostnames)
	}
}

func applyDeviceDefaults(dev *DeviceSpec, def Defaults) {
	if dev.Site == "" {
		dev.Site = def.Site
	}
	if dev.Location == "" {
		dev.Location = def.Location
	}
	if dev.Owner == "" {
		dev.Owner = def.Owner
	}
	if dev.Status == "" && def.Status != "" {
		dev.Status = def.Status
	}
	if len(def.Tags) > 0 {
		dev.Tags = unionStrings(dev.Tags, def.Tags)
	}
}

func unionStrings(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	var out []string
	for _, s := range a {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, s := range b {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func duplicateIssue(kind, key, file string, now time.Time) model.Issue {
	return model.Issue{
		ID:         fmt.Sprintf("dup:%s:%s", kind, key),
		Severity:   "error",
		Kind:       "yaml_validation",
		EntityKind: kind,
		EntityName: key,
		Message:    fmt.Sprintf("Duplicate %s key %q in manual YAML.", kind, key),
		FilePath:   file,
		DetectedAt: now,
	}
}
