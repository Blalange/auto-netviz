package merge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/blalange/auto-netviz/internal/db"
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/manual"
	"github.com/blalange/auto-netviz/internal/model"
	"github.com/blalange/auto-netviz/internal/protocol"
)

type SourceDeviceObs struct {
	ObsID       int64
	CollectorID string
	Site        string
	Source      string
	Profile     string
	ObsKey      string
	ObservedAt  time.Time
	Data        protocol.DeviceObservation
}

type SourceLinkObs struct {
	CollectorID string
	Site        string
	Source      string
	Profile     string
	ObservedAt  time.Time
	Data        protocol.LinkObservation
}

type SourceVLANObs struct {
	CollectorID string
	Source      string
	ObservedAt  time.Time
	Data        protocol.VLANObservation
}

type SourcePrefixObs struct {
	CollectorID string
	Source      string
	ObservedAt  time.Time
	Data        protocol.PrefixObservation
}

type identityCluster struct {
	CanonicalID string
	Manual      *manual.DeviceSpec
	Obs         []SourceDeviceObs
	StrongKeys  []string
	FirstSeen   time.Time
	LastSeen    time.Time
}

type identityNode struct {
	isManual  bool
	manualIdx int
	obsIdx    int
	site      string
	uuids     map[string]struct{}
	serials   map[string]struct{}
	chassis   map[string]struct{}
	macs      map[string]struct{}
	tier3Keys []string
	tier4Keys []string
}

type unionFind struct {
	parent  []int
	manual  []int // index of manual node in component, or -1
	manName []string
	uuids   []map[string]struct{}
	serials []map[string]struct{}
}

func newUnionFind(nodes []identityNode, inv *manual.Inventory) *unionFind {
	n := len(nodes)
	uf := &unionFind{
		parent:  make([]int, n),
		manual:  make([]int, n),
		manName: make([]string, n),
		uuids:   make([]map[string]struct{}, n),
		serials: make([]map[string]struct{}, n),
	}
	for i := 0; i < n; i++ {
		uf.parent[i] = i
		uf.manual[i] = -1
		if nodes[i].isManual {
			uf.manual[i] = nodes[i].manualIdx
			uf.manName[i] = inv.Devices[nodes[i].manualIdx].Name
		}
		uf.uuids[i] = copySet(nodes[i].uuids)
		uf.serials[i] = copySet(nodes[i].serials)
	}
	return uf
}

func (uf *unionFind) find(x int) int {
	for uf.parent[x] != x {
		uf.parent[x] = uf.parent[uf.parent[x]]
		x = uf.parent[x]
	}
	return x
}

// tryUnion merges components containing a and b if no guardrails trip.
// When enforceAllTier1 is true (for Tier 2/3/4 merges), disjoint UUIDs or serials
// block the merge so DHCP IP reuse cannot glue two different machines together.
func (uf *unionFind) tryUnion(a, b int, reasonKey string, enforceAllTier1 bool) (bool, string) {
	ra := uf.find(a)
	rb := uf.find(b)
	if ra == rb {
		return true, ""
	}
	if setsDisjointNonEmpty(uf.uuids[ra], uf.uuids[rb]) {
		return false, ""
	}
	if enforceAllTier1 && setsDisjointNonEmpty(uf.serials[ra], uf.serials[rb]) {
		return false, ""
	}
	if uf.manual[ra] >= 0 && uf.manual[rb] >= 0 {
		return false, fmt.Sprintf("conflicting manual identity %s claimed by both %q and %q", reasonKey, uf.manName[ra], uf.manName[rb])
	}
	// Keep manual root or lower root as parent.
	if uf.manual[rb] >= 0 && uf.manual[ra] < 0 {
		ra, rb = rb, ra
	}
	uf.parent[rb] = ra
	for k := range uf.uuids[rb] {
		uf.uuids[ra][k] = struct{}{}
	}
	for k := range uf.serials[rb] {
		uf.serials[ra][k] = struct{}{}
	}
	return true, ""
}

func resolveDeviceIdentities(
	inv *manual.Inventory,
	devObs []SourceDeviceObs,
	prevIdentities []db.IdentityRecord,
	now time.Time,
) (clusters []identityCluster, obsToCanonical map[int]string, persisted []db.IdentityRecord, issues []model.Issue) {
	nodes := make([]identityNode, 0, len(inv.Devices)+len(devObs))
	for i, d := range inv.Devices {
		nodes = append(nodes, extractManualNode(i, d, inv.Defaults.Site))
	}
	for i, o := range devObs {
		nodes = append(nodes, extractObsNode(i, o, inv.Defaults.Site))
	}

	uf := newUnionFind(nodes, inv)
	seenConflict := make(map[string]struct{})
	addConflict := func(msg string) {
		if _, ok := seenConflict[msg]; ok {
			return
		}
		seenConflict[msg] = struct{}{}
		issues = append(issues, model.Issue{
			ID:         "merge-conflict:" + shortHash(msg),
			Severity:   "error",
			Kind:       "merge_conflict",
			EntityKind: "device",
			Message:    msg,
			DetectedAt: now,
		})
	}

	// Process unions in strict confidence order: Tier 1 -> Tier 2 -> Tier 3 -> Tier 4.
	linkByKey := func(collectKeys func(identityNode) []string, tierLabel string, enforceAllTier1 bool) {
		keyOwners := make(map[string][]int)
		for idx, n := range nodes {
			for _, k := range collectKeys(n) {
				keyOwners[k] = append(keyOwners[k], idx)
			}
		}
		sortedKeys := make([]string, 0, len(keyOwners))
		for k := range keyOwners {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Strings(sortedKeys)
		for _, k := range sortedKeys {
			idxs := keyOwners[k]
			if len(idxs) < 2 {
				continue
			}
			anchor := idxs[0]
			for _, other := range idxs[1:] {
				ok, msg := uf.tryUnion(anchor, other, tierLabel+" "+k, enforceAllTier1)
				if !ok && msg != "" {
					addConflict(msg)
				}
			}
		}
	}

	// Tier 1: UUID, Serial/ServiceTag, ChassisID
	linkByKey(func(n identityNode) []string {
		var out []string
		for k := range n.uuids {
			out = append(out, "uuid:"+k)
		}
		for k := range n.serials {
			out = append(out, "serial:"+k)
		}
		for k := range n.chassis {
			out = append(out, "chassis:"+k)
		}
		return out
	}, "hardware-id", false)

	// Tier 2: Physical non-local MACs
	linkByKey(func(n identityNode) []string {
		var out []string
		for k := range n.macs {
			out = append(out, "mac:"+k)
		}
		return out
	}, "mac", true)

	// Tier 3: Site-scoped locators (snmp_ip, redfish_ip, ipmi_ip, ssh_host, unifi_ip, k8s_node)
	linkByKey(func(n identityNode) []string { return n.tier3Keys }, "locator", true)

	// Tier 4: Site-scoped soft hostnames
	linkByKey(func(n identityNode) []string { return n.tier4Keys }, "hostname", true)

	// Group nodes by union-find root.
	rootNodes := make(map[int][]identityNode)
	for i, n := range nodes {
		r := uf.find(i)
		rootNodes[r] = append(rootNodes[r], n)
	}

	// Build lookup from previous persistent identities (by manual_key or strong keys).
	prevByManual := make(map[string]db.IdentityRecord)
	prevByStrong := make(map[string]db.IdentityRecord)
	for _, rec := range prevIdentities {
		if rec.ManualKey != "" {
			prevByManual[rec.ManualKey] = rec
		}
		for _, sk := range rec.StrongKeys {
			prevByStrong[sk] = rec
		}
	}

	usedIDs := make(map[string]struct{})
	obsToCanonical = make(map[int]string, len(devObs))

	roots := make([]int, 0, len(rootNodes))
	for r := range rootNodes {
		roots = append(roots, r)
	}
	sort.Ints(roots)

	for _, r := range roots {
		group := rootNodes[r]
		var manSpec *manual.DeviceSpec
		var obsList []SourceDeviceObs
		strongSet := make(map[string]struct{})
		allKeysForHash := make([]string, 0)

		firstSeen := now
		lastSeen := time.Time{}

		for _, n := range group {
			for k := range n.uuids {
				strongSet["uuid:"+k] = struct{}{}
			}
			for k := range n.serials {
				strongSet["serial:"+k] = struct{}{}
			}
			for k := range n.chassis {
				strongSet["chassis:"+k] = struct{}{}
			}
			for k := range n.macs {
				strongSet["mac:"+k] = struct{}{}
			}
			if n.isManual {
				spec := inv.Devices[n.manualIdx]
				manSpec = &spec
			} else {
				o := devObs[n.obsIdx]
				obsList = append(obsList, o)
				if o.ObservedAt.After(lastSeen) {
					lastSeen = o.ObservedAt
				}
				if o.ObservedAt.Before(firstSeen) && !o.ObservedAt.IsZero() {
					firstSeen = o.ObservedAt
				}
			}
			allKeysForHash = append(allKeysForHash, n.tier3Keys...)
			allKeysForHash = append(allKeysForHash, n.tier4Keys...)
		}
		if lastSeen.IsZero() {
			lastSeen = now
		}

		strongKeys := sortedKeys(strongSet)
		sort.Strings(allKeysForHash)

		var canonicalID string
		var origin string
		var manualKey string

		if manSpec != nil {
			manualKey = manSpec.Name
			if prev, ok := prevByManual[manualKey]; ok && prev.CanonicalID != "" {
				canonicalID = prev.CanonicalID
				if !prev.FirstSeenAt.IsZero() && prev.FirstSeenAt.Before(firstSeen) {
					firstSeen = prev.FirstSeenAt
				}
			} else {
				slug := identity.Slugify(manSpec.Name)
				if slug == "" {
					slug = shortHash(manSpec.Name)
				}
				canonicalID = "dev-" + slug
			}
			if len(obsList) > 0 {
				origin = "merged"
			} else {
				origin = "manual"
			}
		} else {
			origin = "auto"
			// Match existing persisted identity by strongest key so auto IDs stay stable!
			for _, sk := range strongKeys {
				if prev, ok := prevByStrong[sk]; ok && prev.CanonicalID != "" {
					canonicalID = prev.CanonicalID
					if !prev.FirstSeenAt.IsZero() && prev.FirstSeenAt.Before(firstSeen) {
						firstSeen = prev.FirstSeenAt
					}
					break
				}
			}
			if canonicalID == "" {
				seed := ""
				if len(strongKeys) > 0 {
					seed = strongKeys[0]
				} else if len(allKeysForHash) > 0 {
					seed = allKeysForHash[0]
				} else if len(obsList) > 0 {
					seed = obsList[0].CollectorID + ":" + obsList[0].Source + ":" + obsList[0].ObsKey
				}
				canonicalID = "auto-" + shortHash(seed)
			}
		}

		// Guarantee uniqueness even if two manual devices slugify identically.
		baseID := canonicalID
		suffix := 2
		for {
			if _, exists := usedIDs[canonicalID]; !exists {
				usedIDs[canonicalID] = struct{}{}
				break
			}
			canonicalID = fmt.Sprintf("%s-%d", baseID, suffix)
			suffix++
		}

		for _, n := range group {
			if !n.isManual {
				obsToCanonical[n.obsIdx] = canonicalID
			}
		}

		displayName := ""
		if manSpec != nil {
			displayName = manSpec.Name
		} else if len(obsList) > 0 {
			displayName = obsList[0].Data.Name
		}

		clusters = append(clusters, identityCluster{
			CanonicalID: canonicalID,
			Manual:      manSpec,
			Obs:         obsList,
			StrongKeys:  strongKeys,
			FirstSeen:   firstSeen,
			LastSeen:    lastSeen,
		})

		persisted = append(persisted, db.IdentityRecord{
			CanonicalID: canonicalID,
			EntityKind:  "device",
			Origin:      origin,
			ManualKey:   manualKey,
			StrongKeys:  strongKeys,
			DisplayName: displayName,
			FirstSeenAt: firstSeen,
			LastSeenAt:  lastSeen,
		})
	}

	return clusters, obsToCanonical, persisted, issues
}

func extractManualNode(idx int, d manual.DeviceSpec, defaultSite string) identityNode {
	site := d.Site
	if site == "" {
		site = defaultSite
	}
	n := identityNode{
		isManual:  true,
		manualIdx: idx,
		site:      site,
		uuids:     make(map[string]struct{}),
		serials:   make(map[string]struct{}),
		chassis:   make(map[string]struct{}),
		macs:      make(map[string]struct{}),
	}

	addSerial := func(raw string) {
		if s := identity.NormalizeSerial(raw); s != "" {
			n.serials[s] = struct{}{}
		}
	}
	addUUID := func(raw string) {
		if u := identity.NormalizeUUID(raw); u != "" {
			n.uuids[u] = struct{}{}
		}
	}
	addMAC := func(raw string) {
		if identity.IsInvalidMAC(raw) || identity.IsLocallyAdministeredMAC(raw) {
			return
		}
		if m, err := identity.NormalizeMAC(raw); err == nil {
			n.macs[m] = struct{}{}
		}
	}

	addSerial(d.Serial)
	if d.Identity != nil {
		addSerial(d.Identity.Serial)
		addSerial(d.Identity.ServiceTag)
		addUUID(d.Identity.SystemUUID)
		if d.Identity.ChassisID != "" {
			if m, err := identity.NormalizeMAC(d.Identity.ChassisID); err == nil {
				n.chassis[m] = struct{}{}
			} else {
				n.chassis[strings.ToLower(strings.TrimSpace(d.Identity.ChassisID))] = struct{}{}
			}
		}
		for _, m := range d.Identity.MACs {
			addMAC(m)
		}
	}
	for _, iface := range d.Interfaces {
		if !isVirtualInterfaceName(iface.Name) && iface.Kind != "virtual" && iface.Kind != "lag" {
			addMAC(iface.MACAddress)
		}
		for _, ip := range iface.IPAddresses {
			if normIP := identity.NormalizeIP(ip.Address); normIP != "" {
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ip:%s:%s", site, normIP))
			}
		}
	}
	if d.OOBIP != "" {
		if normIP := identity.NormalizeIP(d.OOBIP); normIP != "" {
			n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("oob:%s:%s", site, normIP))
			n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ip:%s:%s", site, normIP))
		}
	}

	if d.Collect != nil {
		if d.Collect.SNMP != nil && d.Collect.SNMP.IP != "" {
			if ip := identity.NormalizeIP(d.Collect.SNMP.IP); ip != "" {
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("snmp:%s:%s", site, ip))
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ip:%s:%s", site, ip))
			}
		}
		if d.Collect.Redfish != nil && d.Collect.Redfish.IP != "" {
			if ip := identity.NormalizeIP(d.Collect.Redfish.IP); ip != "" {
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("redfish:%s:%s", site, ip))
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("oob:%s:%s", site, ip))
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ip:%s:%s", site, ip))
			}
		}
		if d.Collect.IPMI != nil && d.Collect.IPMI.IP != "" {
			if ip := identity.NormalizeIP(d.Collect.IPMI.IP); ip != "" {
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ipmi:%s:%s", site, ip))
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("oob:%s:%s", site, ip))
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ip:%s:%s", site, ip))
			}
		}
		if d.Collect.SSH != nil {
			host := d.Collect.SSH.Host
			if host == "" {
				host = d.Collect.SSH.IP
			}
			if ip := identity.NormalizeIP(host); ip != "" {
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ssh:%s:%s", site, ip))
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ip:%s:%s", site, ip))
			}
		}
		if d.Collect.UniFi != nil {
			addMAC(d.Collect.UniFi.MAC)
			addSerial(d.Collect.UniFi.Serial)
			if ip := identity.NormalizeIP(d.Collect.UniFi.IP); ip != "" {
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("unifi:%s:%s", site, ip))
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ip:%s:%s", site, ip))
			}
		}
		if d.Collect.Kubernetes != nil && d.Collect.Kubernetes.Node != "" {
			cluster := strings.ToLower(strings.TrimSpace(d.Collect.Kubernetes.Cluster))
			node := identity.ShortHostname(d.Collect.Kubernetes.Node)
			if cluster != "" {
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("k8s:%s:%s", cluster, node))
			} else {
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("k8s-site:%s:%s", site, node))
			}
		}
	}

	if d.Name != "" {
		if soft := identity.NormalizeSoftName(identity.ShortHostname(d.Name)); soft != "" {
			n.tier4Keys = append(n.tier4Keys, fmt.Sprintf("name:%s:%s", site, soft))
		}
		if softFull := identity.NormalizeSoftName(d.Name); softFull != "" {
			n.tier4Keys = append(n.tier4Keys, fmt.Sprintf("full:%s:%s", site, softFull))
		}
	}

	return n
}

func extractObsNode(idx int, o SourceDeviceObs, defaultSite string) identityNode {
	d := o.Data
	site := d.Site
	if site == "" {
		site = o.Site
	}
	if site == "" {
		site = defaultSite
	}
	n := identityNode{
		isManual: false,
		obsIdx:   idx,
		site:     site,
		uuids:    make(map[string]struct{}),
		serials:  make(map[string]struct{}),
		chassis:  make(map[string]struct{}),
		macs:     make(map[string]struct{}),
	}

	addSerial := func(raw string) {
		if s := identity.NormalizeSerial(raw); s != "" {
			n.serials[s] = struct{}{}
		}
	}
	addUUID := func(raw string) {
		if u := identity.NormalizeUUID(raw); u != "" {
			n.uuids[u] = struct{}{}
		}
	}
	addMAC := func(raw string) {
		if identity.IsInvalidMAC(raw) || identity.IsLocallyAdministeredMAC(raw) {
			return
		}
		if m, err := identity.NormalizeMAC(raw); err == nil {
			n.macs[m] = struct{}{}
		}
	}

	addUUID(d.SystemUUID)
	for _, u := range d.Claims.UUIDs {
		addUUID(u)
	}
	addSerial(d.Serial)
	addSerial(d.ServiceTag)
	for _, s := range d.Claims.Serials {
		addSerial(s)
	}
	if d.ChassisID != "" {
		if m, err := identity.NormalizeMAC(d.ChassisID); err == nil {
			n.chassis[m] = struct{}{}
			addMAC(m)
		} else {
			n.chassis[strings.ToLower(strings.TrimSpace(d.ChassisID))] = struct{}{}
		}
	}
	for _, c := range d.Claims.ChassisIDs {
		if m, err := identity.NormalizeMAC(c); err == nil {
			n.chassis[m] = struct{}{}
			addMAC(m)
		} else if s := strings.ToLower(strings.TrimSpace(c)); s != "" {
			n.chassis[s] = struct{}{}
		}
	}

	for _, m := range d.Claims.MACs {
		addMAC(m)
	}
	for _, iface := range d.Interfaces {
		if iface.Kind == "virtual" || iface.Kind == "lag" || isVirtualInterfaceName(iface.Name) {
			continue
		}
		addMAC(iface.MAC)
	}

	for kind, val := range d.Claims.Locators {
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		switch kind {
		case "snmp_ip", "redfish_ip", "ipmi_ip", "ssh_host", "unifi_ip", "oob_ip", "mgmt_ip":
			prefix := strings.TrimSuffix(kind, "_ip")
			prefix = strings.TrimSuffix(prefix, "_host")
			if ip := identity.NormalizeIP(val); ip != "" {
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("%s:%s:%s", prefix, site, ip))
				if prefix == "redfish" || prefix == "ipmi" {
					n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("oob:%s:%s", site, ip))
				} else if prefix == "snmp" || prefix == "unifi" || prefix == "ssh" || prefix == "mgmt" {
					n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ip:%s:%s", site, ip))
				}
			}
		case "k8s_node":
			node := identity.ShortHostname(val)
			cluster := strings.ToLower(strings.TrimSpace(d.Claims.Locators["cluster"]))
			if cluster == "" {
				cluster = strings.ToLower(strings.TrimSpace(d.Cluster))
			}
			if cluster != "" {
				n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("k8s:%s:%s", cluster, node))
			}
			n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("k8s-site:%s:%s", site, node))
		}
	}

	if ip := identity.NormalizeIP(d.PrimaryIP); ip != "" {
		n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ip:%s:%s", site, ip))
	}
	if ip := identity.NormalizeIP(d.MgmtIP); ip != "" {
		n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("ip:%s:%s", site, ip))
	}
	if ip := identity.NormalizeIP(d.OOBIP); ip != "" {
		n.tier3Keys = append(n.tier3Keys, fmt.Sprintf("oob:%s:%s", site, ip))
	}

	for _, host := range append([]string{d.Name}, d.Claims.Hostnames...) {
		if host == "" || isGenericHostname(host) {
			continue
		}
		if soft := identity.NormalizeSoftName(identity.ShortHostname(host)); soft != "" && !isGenericHostname(soft) {
			n.tier4Keys = append(n.tier4Keys, fmt.Sprintf("name:%s:%s", site, soft))
		}
		if softFull := identity.NormalizeSoftName(host); softFull != "" && !isGenericHostname(softFull) {
			n.tier4Keys = append(n.tier4Keys, fmt.Sprintf("full:%s:%s", site, softFull))
		}
	}

	return n
}

func isVirtualInterfaceName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	for _, prefix := range []string{
		"lo", "veth", "docker", "br-", "cni", "flannel", "cilium", "tunl",
		"wg", "virbr", "tap", "kube-", "dummy", "bond", "vlan", "sit", "ip6tnl",
	} {
		if lower == prefix || strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func setsDisjointNonEmpty(a, b map[string]struct{}) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	for k := range a {
		if _, ok := b[k]; ok {
			return false
		}
	}
	return true
}

func copySet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isGenericHostname(raw string) bool {
	norm := identity.NormalizeSoftName(raw)
	short := identity.NormalizeSoftName(identity.ShortHostname(raw))
	for _, v := range []string{norm, short} {
		switch v {
		case "dellinc", "dell", "localhost", "localhostlocaldomain", "idrac",
			"ubuntu", "debian", "linux", "talos", "unifi", "unifilocaldomain",
			"server", "switch", "gateway", "router", "default", "unknown",
			"quantacomputerinc", "supermicro", "hp", "hpe":
			return true
		}
	}
	return false
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}
