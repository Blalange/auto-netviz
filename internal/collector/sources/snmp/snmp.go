package snmp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blalange/auto-netviz/internal/collector/config"
	"github.com/blalange/auto-netviz/internal/collector/sources"
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/protocol"
	"github.com/gosnmp/gosnmp"
)

const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysObjectID = "1.3.6.1.2.1.1.2.0"
	oidSysContact  = "1.3.6.1.2.1.1.4.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
	oidSysLocation = "1.3.6.1.2.1.1.6.0"
	oidSysServices = "1.3.6.1.2.1.1.7.0"

	oidIfDescr       = "1.3.6.1.2.1.2.2.1.2"
	oidIfType        = "1.3.6.1.2.1.2.2.1.3"
	oidIfMTU         = "1.3.6.1.2.1.2.2.1.4"
	oidIfPhysAddress = "1.3.6.1.2.1.2.2.1.6"
	oidIfAdminStatus = "1.3.6.1.2.1.2.2.1.7"
	oidIfOperStatus  = "1.3.6.1.2.1.2.2.1.8"
	oidIfName        = "1.3.6.1.2.1.31.1.1.1.1"
	oidIfHighSpeed   = "1.3.6.1.2.1.31.1.1.1.15"
	oidIfAlias       = "1.3.6.1.2.1.31.1.1.1.18"

	oidBridgeBaseAddr  = "1.3.6.1.2.1.17.1.1.0"
	oidBridgePortIfIdx = "1.3.6.1.2.1.17.1.4.1.2"
	oidFdbAddress      = "1.3.6.1.2.1.17.4.3.1.1"
	oidFdbPort         = "1.3.6.1.2.1.17.4.3.1.2"
	oidVlanName        = "1.3.6.1.2.1.17.7.1.4.3.1.1"
	oidVlanEgress      = "1.3.6.1.2.1.17.7.1.4.3.1.2"
	oidVlanUntagged    = "1.3.6.1.2.1.17.7.1.4.3.1.4"
	oidPVID            = "1.3.6.1.2.1.17.7.1.4.5.1.1"

	oidEntityDescr  = "1.3.6.1.2.1.47.1.1.1.1.2"
	oidEntitySerial = "1.3.6.1.2.1.47.1.1.1.1.11"
	oidEntityMfg    = "1.3.6.1.2.1.47.1.1.1.1.12"
	oidEntityModel  = "1.3.6.1.2.1.47.1.1.1.1.13"

	oidLLDPLocPortID   = "1.0.8802.1.1.2.1.3.7.1.3"
	oidLLDPLocPortDesc = "1.0.8802.1.1.2.1.3.7.1.4"
	oidLLDPRemChassis  = "1.0.8802.1.1.2.1.4.1.1.5"
	oidLLDPRemPortID   = "1.0.8802.1.1.2.1.4.1.1.7"
	oidLLDPRemPortDesc = "1.0.8802.1.1.2.1.4.1.1.8"
	oidLLDPRemSysName  = "1.0.8802.1.1.2.1.4.1.1.9"

	oidQBridgeFdbPort  = "1.3.6.1.2.1.17.7.1.2.2.1.2"
	oidLagAttachedAgg  = "1.2.840.10006.300.43.1.2.1.1.13"
	oidCiscoVtpVlan    = "1.3.6.1.4.1.9.9.46.1.3.1.1.4"
	oidCiscoVmVlan     = "1.3.6.1.4.1.9.9.68.1.2.2.1.2"
	oidCiscoTrunkStat  = "1.3.6.1.4.1.9.9.46.1.6.1.1.14"

	oidCDPCacheDevID = "1.3.6.1.4.1.9.9.23.1.2.1.1.6"
	oidCDPCachePort  = "1.3.6.1.4.1.9.9.23.1.2.1.1.7"
)

var (
	rePortNumber = regexp.MustCompile(`(?i)^Port\s+(\d+)$`)
	reVirtualIf  = regexp.MustCompile(`(?i)^(?:lo|vlan|br|bond|lag|po|port-channel|mgmt|enif|cpu|tunnel|ipmi|docker|veth|virbr)\d*`)
)

type Source struct {
	Profiles []config.SNMPProfile
}

func (s *Source) Name() string { return "snmp" }

func (s *Source) Enabled() bool {
	for _, p := range s.Profiles {
		if config.IsEnabled(p.Enabled) {
			return true
		}
	}
	return false
}

type targetJob struct {
	ip         string
	name       string
	site       string
	role       string
	tags       []string
	explicit   string
	portNames  string
	ifacesMode bool
	cablesMode bool
	autoUniFi  bool
	profile    config.SNMPProfile
}

func (s *Source) Collect(ctx context.Context, rt sources.Runtime) ([]protocol.ObservationBatch, []protocol.SourceHealthPayload, error) {
	seen := make(map[string]struct{})
	var jobs []targetJob

	addTarget := func(ip, name, site, role string, tags []string, explicit, portMode string, autoUniFi bool, profOverride *config.SNMPProfile) {
		ip = identity.NormalizeIP(ip)
		if ip == "" {
			return
		}
		if site == "" {
			site = rt.Site
		}
		key := site + "|" + ip
		if _, ok := seen[key]; ok {
			return
		}
		var prof *config.SNMPProfile
		var err error
		if profOverride != nil {
			prof = profOverride
		} else {
			prof, err = config.MatchSNMPProfile(s.Profiles, explicit, site, name, role, tags, ip)
			if err != nil {
				rt.Log.Warn("snmp profile match failed", "target", name, "ip", ip, "err", err)
				return
			}
		}
		if portMode == "" {
			portMode = prof.PortNames
		}
		if portMode == "" {
			portMode = "auto"
		}
		seen[key] = struct{}{}
		jobs = append(jobs, targetJob{
			ip: ip, name: name, site: site, role: role, tags: tags,
			portNames: portMode, autoUniFi: autoUniFi, profile: *prof,
		})
	}

	// 1. Static profile targets
	for _, p := range s.Profiles {
		if !config.IsEnabled(p.Enabled) {
			continue
		}
		for _, t := range p.Targets {
			ip := t.IP
			if ip == "" {
				ip = t.Host
			}
			pCopy := p
			addTarget(ip, t.Name, t.Site, t.Role, nil, p.Name, p.PortNames, false, &pCopy)
		}
	}
	// 2. Manual YAML hints
	for _, h := range rt.Hints {
		if h.SNMPIP != "" {
			addTarget(h.SNMPIP, h.DeviceName, h.Site, h.Role, h.Tags, h.SNMPCredential, "", false, nil)
		}
	}
	// 3. Active UniFi targets (auto_unifi)
	if rt.Shared != nil {
		for _, u := range rt.Shared.UniFi() {
			prof, err := config.MatchSNMPProfile(s.Profiles, u.SNMPCredential, u.Site, u.Name, "switch", nil, u.IP)
			if err == nil && config.IsEnabled(prof.AutoUniFi) {
				addTarget(u.IP, u.Name, u.Site, "switch", nil, prof.Name, prof.PortNames, true, prof)
			}
		}
		// 4. Discovered SNMP endpoints
		for _, pr := range rt.Shared.Probed() {
			if pr.HasSNMP {
				addTarget(pr.IP, "", pr.Site, "switch", nil, "", "", false, nil)
			}
		}
	}

	if len(jobs) == 0 {
		return nil, []protocol.SourceHealthPayload{{
			Source: "snmp", Profile: "default", Enabled: true, Status: "ok",
		}}, nil
	}

	// Group by profile name
	byProf := make(map[string][]targetJob)
	for _, j := range jobs {
		byProf[j.profile.Name] = append(byProf[j.profile.Name], j)
	}

	var batches []protocol.ObservationBatch
	var healths []protocol.SourceHealthPayload

	for profName, pJobs := range byProf {
		start := time.Now()
		prof := pJobs[0].profile
		h := protocol.SourceHealthPayload{
			Source: "snmp", Profile: profName, Enabled: true, LastRunAt: &start,
		}

		community, err := prof.Community.Resolve(ctx, rt.BaseDir, rt.K8s, "")
		if prof.Version != "3" && (err != nil || community == "") {
			community = "public"
		}

		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 6)
		var devs []protocol.DeviceObservation
		var links []protocol.LinkObservation
		var vlans []protocol.VLANObservation
		var errs []string

		for _, job := range pJobs {
			wg.Add(1)
			go func(j targetJob) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				obs, err := pollDevice(ctx, rt.BaseDir, rt.K8s, j, community)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if j.autoUniFi && isExpectedSNMPUnavailable(err) {
						rt.Log.Debug("skipping auto-unifi target without SNMP", "target", j.name, "ip", j.ip, "err", err)
						return
					}
					errs = append(errs, fmt.Sprintf("%s: %v", j.ip, err))
					return
				}
				devs = append(devs, obs.Device)
				links = append(links, obs.Links...)
				vlans = append(vlans, obs.VLANs...)
			}(job)
		}
		wg.Wait()

		batch := protocol.ObservationBatch{
			BatchID:      fmt.Sprintf("snmp-%s-%d", profName, time.Now().UnixNano()),
			CollectorID:  rt.CollectorID,
			Site:         rt.Site,
			Source:       "snmp",
			Profile:      profName,
			CollectedAt:  time.Now().UTC(),
			TTLSeconds:   600,
			FullSnapshot: true,
			Devices:      devs,
			Links:        links,
			VLANs:        vlans,
			Errors:       errs,
		}
		batches = append(batches, batch)

		done := time.Now()
		h.LastSuccessAt = &done
		h.DurationMs = time.Since(start).Milliseconds()
		h.DeviceCount = len(devs)
		h.LinkCount = len(links)
		if len(errs) > 0 && len(devs) == 0 {
			h.Status = "error"
			h.Error = strings.Join(errs, "; ")
		} else {
			h.Status = "ok"
			if len(errs) > 0 {
				h.Error = fmt.Sprintf("%d partial error(s): %s", len(errs), errs[0])
			}
		}
		healths = append(healths, h)
	}
	return batches, healths, nil
}

type pollResult struct {
	Device protocol.DeviceObservation
	Links  []protocol.LinkObservation
	VLANs  []protocol.VLANObservation
}

func pollDevice(ctx context.Context, baseDir string, k8s *config.K8sResolver, job targetJob, community string) (*pollResult, error) {
	timeout := config.ParseDuration(job.profile.Timeout, 2*time.Second)
	retries := job.profile.Retries
	if retries <= 0 {
		retries = 1
	}

	g := &gosnmp.GoSNMP{
		Target:    job.ip,
		Port:      161,
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   timeout,
		Retries:   retries,
		Context:   ctx,
	}

	if job.profile.Version == "3" {
		user, _ := job.profile.Username.Resolve(ctx, baseDir, k8s, "")
		authPass, _ := job.profile.AuthPass.Resolve(ctx, baseDir, k8s, "")
		privPass, _ := job.profile.PrivPass.Resolve(ctx, baseDir, k8s, "")
		g.Version = gosnmp.Version3
		g.SecurityModel = gosnmp.UserSecurityModel
		secFlags := gosnmp.NoAuthNoPriv
		authProto := gosnmp.NoAuth
		if authPass != "" {
			secFlags = gosnmp.AuthNoPriv
			switch strings.ToUpper(job.profile.AuthProtocol) {
			case "MD5":
				authProto = gosnmp.MD5
			case "SHA256":
				authProto = gosnmp.SHA256
			default:
				authProto = gosnmp.SHA
			}
		}
		privProto := gosnmp.NoPriv
		if privPass != "" {
			secFlags = gosnmp.AuthPriv
			switch strings.ToUpper(job.profile.PrivProtocol) {
			case "DES":
				privProto = gosnmp.DES
			default:
				privProto = gosnmp.AES
			}
		}
		g.MsgFlags = secFlags
		g.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 user,
			AuthenticationProtocol:   authProto,
			AuthenticationPassphrase: authPass,
			PrivacyProtocol:          privProto,
			PrivacyPassphrase:        privPass,
		}
	}

	if err := g.Connect(); err != nil {
		return nil, err
	}
	defer g.Conn.Close()

	sysRes, err := g.Get([]string{oidSysDescr, oidSysObjectID, oidSysName, oidSysLocation, oidSysServices, oidBridgeBaseAddr})
	if err != nil {
		return nil, err
	}
	sysDescr, sysObjID, sysName, baseMAC := "", "", "", ""
	var sysServices int
	for _, pdu := range sysRes.Variables {
		switch strings.TrimPrefix(pdu.Name, ".") {
		case oidSysDescr:
			sysDescr = pduText(pdu)
		case oidSysObjectID:
			sysObjID = pduText(pdu)
		case oidSysName:
			sysName = pduText(pdu)
		case oidSysServices:
			sysServices = pduInt(pdu)
		case oidBridgeBaseAddr:
			baseMAC = pduMAC(pdu)
		}
	}
	if sysDescr == "" && sysName == "" {
		return nil, fmt.Errorf("empty SNMP system response")
	}

	walkTable := func(baseOID string) map[string]gosnmp.SnmpPDU {
		out := make(map[string]gosnmp.SnmpPDU)
		_ = g.BulkWalk(baseOID, func(pdu gosnmp.SnmpPDU) error {
			name := strings.TrimPrefix(pdu.Name, ".")
			out[name] = pdu
			return nil
		})
		return out
	}

	ifDescrs := walkTable(oidIfDescr)
	ifNames := walkTable(oidIfName)
	ifAliases := walkTable(oidIfAlias)
	ifTypes := walkTable(oidIfType)
	ifMTUs := walkTable(oidIfMTU)
	ifMACs := walkTable(oidIfPhysAddress)
	ifAdmins := walkTable(oidIfAdminStatus)
	ifOpers := walkTable(oidIfOperStatus)
	ifSpeeds := walkTable(oidIfHighSpeed)

	entSerials := walkTable(oidEntitySerial)
	entModels := walkTable(oidEntityModel)
	entMfgs := walkTable(oidEntityMfg)

	portIfIdxRaw := walkTable(oidBridgePortIfIdx)
	vlanNamesRaw := walkTable(oidVlanName)
	vlanEgressRaw := walkTable(oidVlanEgress)
	vlanUntaggedRaw := walkTable(oidVlanUntagged)
	pvidRaw := walkTable(oidPVID)

	lldpLocIDs := walkTable(oidLLDPLocPortID)
	lldpLocDescs := walkTable(oidLLDPLocPortDesc)
	lldpRemChassis := walkTable(oidLLDPRemChassis)
	lldpRemPortIDs := walkTable(oidLLDPRemPortID)
	lldpRemPortDescs := walkTable(oidLLDPRemPortDesc)
	lldpRemSysNames := walkTable(oidLLDPRemSysName)

	fdbAddrs := walkTable(oidFdbAddress)
	fdbPorts := walkTable(oidFdbPort)
	qFdbPorts := walkTable(oidQBridgeFdbPort)
	lagAttachedRaw := walkTable(oidLagAttachedAgg)
	ciscoVtpVlansRaw := walkTable(oidCiscoVtpVlan)
	ciscoVmVlansRaw := walkTable(oidCiscoVmVlan)
	ciscoTrunkRaw := walkTable(oidCiscoTrunkStat)

	cdpDevIDs := walkTable(oidCDPCacheDevID)
	cdpPorts := walkTable(oidCDPCachePort)

	// Build interface list keyed by ifIndex
	type rawIf struct {
		idx                        int
		name, alias, descr, mac    string
		ifType, mtu, admin, oper   int
		speed                      int
		candidates                 []string
	}
	ifMap := make(map[int]*rawIf)
	ensureIf := func(idx int) *rawIf {
		if r, ok := ifMap[idx]; ok {
			return r
		}
		r := &rawIf{idx: idx}
		ifMap[idx] = r
		return r
	}
	for oid, pdu := range ifNames {
		if idx := lastIntSuffix(oid, oidIfName); idx > 0 {
			ensureIf(idx).name = pduText(pdu)
		}
	}
	for oid, pdu := range ifDescrs {
		if idx := lastIntSuffix(oid, oidIfDescr); idx > 0 {
			ensureIf(idx).descr = pduText(pdu)
		}
	}
	for oid, pdu := range ifAliases {
		if idx := lastIntSuffix(oid, oidIfAlias); idx > 0 {
			ensureIf(idx).alias = pduText(pdu)
		}
	}
	for oid, pdu := range ifTypes {
		if idx := lastIntSuffix(oid, oidIfType); idx > 0 {
			ensureIf(idx).ifType = pduInt(pdu)
		}
	}
	for oid, pdu := range ifMTUs {
		if idx := lastIntSuffix(oid, oidIfMTU); idx > 0 {
			ensureIf(idx).mtu = pduInt(pdu)
		}
	}
	for oid, pdu := range ifMACs {
		if idx := lastIntSuffix(oid, oidIfPhysAddress); idx > 0 {
			ensureIf(idx).mac = pduMAC(pdu)
		}
	}
	for oid, pdu := range ifAdmins {
		if idx := lastIntSuffix(oid, oidIfAdminStatus); idx > 0 {
			ensureIf(idx).admin = pduInt(pdu)
		}
	}
	for oid, pdu := range ifOpers {
		if idx := lastIntSuffix(oid, oidIfOperStatus); idx > 0 {
			ensureIf(idx).oper = pduInt(pdu)
		}
	}
	for oid, pdu := range ifSpeeds {
		if idx := lastIntSuffix(oid, oidIfHighSpeed); idx > 0 {
			ensureIf(idx).speed = pduInt(pdu)
		}
	}

	// Bridge port -> ifIndex and VLAN maps
	portIfIdx := make(map[int]int)
	for oid, pdu := range portIfIdxRaw {
		if bp := lastIntSuffix(oid, oidBridgePortIfIdx); bp > 0 {
			portIfIdx[bp] = pduInt(pdu)
		}
	}
	vlanNames := make(map[int]string)
	for oid, pdu := range vlanNamesRaw {
		if vid := lastIntSuffix(oid, oidVlanName); vid > 0 {
			vlanNames[vid] = pduText(pdu)
		}
	}
	for oid, pdu := range ciscoVtpVlansRaw {
		if vid := lastCiscoVlanSuffix(oid, oidCiscoVtpVlan); vid > 0 && (vid < 1002 || vid > 1005) {
			if _, exists := vlanNames[vid]; !exists {
				if vName := pduText(pdu); vName != "" {
					vlanNames[vid] = vName
				}
			}
		}
	}

	ciscoTrunk := make(map[int]bool)
	for oid, pdu := range ciscoTrunkRaw {
		if idx := lastIntSuffix(oid, oidCiscoTrunkStat); idx > 0 && pduInt(pdu) == 1 {
			ciscoTrunk[idx] = true
		}
	}

	egress := make(map[int]map[int]bool)
	for oid, pdu := range vlanEgressRaw {
		if vid := lastIntSuffix(oid, oidVlanEgress); vid > 0 {
			egress[vid] = parseBitmap(pduBytes(pdu))
		}
	}
	untagged := make(map[int]map[int]bool)
	for oid, pdu := range vlanUntaggedRaw {
		if vid := lastIntSuffix(oid, oidVlanUntagged); vid > 0 {
			untagged[vid] = parseBitmap(pduBytes(pdu))
		}
	}
	pvidByPort := make(map[int]int)
	for oid, pdu := range pvidRaw {
		if bp := lastIntSuffix(oid, oidPVID); bp > 0 {
			pvidByPort[bp] = pduInt(pdu)
		}
	}

	ifVLANs := make(map[int]struct {
		untagged int
		tagged   []int
	})
	for bp, idx := range portIfIdx {
		if _, exists := ifMap[idx]; !exists {
			continue
		}
		egressVids := make(map[int]bool)
		for vid, ports := range egress {
			if ports[bp] {
				egressVids[vid] = true
			}
		}
		untaggedVids := make(map[int]bool)
		for vid, ports := range untagged {
			if ports[bp] && (len(egressVids) == 0 || egressVids[vid]) {
				untaggedVids[vid] = true
			}
		}
		if pv := pvidByPort[bp]; pv > 0 && (len(egressVids) == 0 || egressVids[pv]) {
			untaggedVids = map[int]bool{pv: true}
		}
		var uList []int
		for vid := range untaggedVids {
			if vid > 0 {
				uList = append(uList, vid)
			}
		}
		sort.Ints(uList)
		var tList []int
		for vid := range egressVids {
			if vid > 0 && !untaggedVids[vid] {
				tList = append(tList, vid)
			}
		}
		var uVID int
		if len(uList) > 0 {
			uVID = uList[0]
			if len(uList) > 1 {
				tList = append(tList, uList[1:]...)
			}
		}
		sort.Ints(tList)
		ifVLANs[idx] = struct {
			untagged int
			tagged   []int
		}{untagged: uVID, tagged: tList}
	}
	for oid, pdu := range ciscoVmVlansRaw {
		if idx := lastIntSuffix(oid, oidCiscoVmVlan); idx > 0 {
			if vid := pduInt(pdu); vid > 0 {
				cur := ifVLANs[idx]
				if cur.untagged == 0 {
					cur.untagged = vid
					ifVLANs[idx] = cur
				}
			}
		}
	}

	lagMembers := make(map[int][]int)
	for oid, pdu := range lagAttachedRaw {
		memIdx := lastIntSuffix(oid, oidLagAttachedAgg)
		aggIdx := pduInt(pdu)
		if memIdx > 0 && aggIdx > 0 {
			if memIf, ok := ifMap[memIdx]; ok && memIf.oper == 1 {
				lagMembers[aggIdx] = append(lagMembers[aggIdx], memIdx)
			}
		}
	}
	for aggIdx, mems := range lagMembers {
		sort.Slice(mems, func(i, j int) bool {
			ni, nj := identity.PortSortNumber(ifMap[mems[i]].name), identity.PortSortNumber(ifMap[mems[j]].name)
			if ni != nj && ni > 0 && nj > 0 {
				return ni < nj
			}
			return mems[i] < mems[j]
		})
		lagMembers[aggIdx] = mems
	}

	macCounts := make(map[string]int)
	switchMACs := make(map[string]bool)
	if baseMAC != "" {
		switchMACs[baseMAC] = true
	}
	var indices []int
	for idx, r := range ifMap {
		indices = append(indices, idx)
		r.candidates = identity.PortCandidates(r.name, r.alias, r.descr)
		if r.mac != "" {
			macCounts[r.mac]++
			switchMACs[r.mac] = true
		}
	}
	sort.Ints(indices)

	var ifaces []protocol.InterfaceObservation
	ifNameByIdx := make(map[int]string)
	for _, idx := range indices {
		r := ifMap[idx]
		portName := pickPortName(r.name, r.alias, r.descr, r.candidates, job.portNames)
		if portName == "" {
			continue
		}
		ifNameByIdx[idx] = portName
		en := r.admin != 2
		up := r.oper == 1
		vInfo := ifVLANs[idx]
		isTrunk := len(vInfo.tagged) > 0 || ciscoTrunk[idx]
		mode := ""
		if isTrunk {
			mode = "tagged"
		} else if vInfo.untagged > 0 {
			mode = "access"
		}
		mac := ""
		if r.mac != "" && r.mac != baseMAC && macCounts[r.mac] == 1 {
			mac = r.mac
		}
		ifaces = append(ifaces, protocol.InterfaceObservation{
			Name:         portName,
			Candidates:   r.candidates,
			IfIndex:      idx,
			Type:         inferSNMPIfType(r.name, r.descr, r.alias, r.ifType, r.speed),
			Kind:         "physical",
			Enabled:      &en,
			LinkUp:       &up,
			SpeedMbps:    r.speed,
			MTU:          r.mtu,
			MAC:          mac,
			Description:  r.alias,
			Mode:         mode,
			UntaggedVLAN: vInfo.untagged,
			TaggedVLANs:  vInfo.tagged,
			IsTrunk:      isTrunk,
		})
	}

	serial := firstNonEmptyVal(entSerials)
	modelName := firstNonEmptyVal(entModels)
	mfg := firstNonEmptyVal(entMfgs)
	if mfg == "" {
		mfg = inferSNMPVendor(sysObjID, sysDescr, baseMAC)
	}
	role := inferSNMPRole(sysServices, sysDescr, job.role)
	devName := job.name
	if devName == "" {
		devName = sysName
	}

	claims := protocol.IdentityClaims{
		Locators: map[string]string{"snmp_ip": job.ip},
	}
	if serial != "" {
		claims.Serials = []string{serial}
	}
	if baseMAC != "" {
		claims.MACs = []string{baseMAC}
	}
	if sysName != "" {
		claims.Hostnames = []string{sysName}
	}

	dev := protocol.DeviceObservation{
		ObsKey:       job.ip,
		Site:         job.site,
		Name:         devName,
		RoleHint:     role,
		Manufacturer: mfg,
		Model:        modelName,
		Status:       "active",
		Serial:       serial,
		Description:  sysDescr,
		PrimaryIP:    job.ip,
		MgmtIP:       job.ip,
		Claims:       claims,
		Interfaces:   ifaces,
	}

	// VLANs
	var vlans []protocol.VLANObservation
	for vid, vName := range vlanNames {
		if vid <= 0 {
			continue
		}
		vlans = append(vlans, protocol.VLANObservation{
			Site: job.site, VID: vid, Name: vName, Description: "SNMP-discovered VLAN",
		})
	}

	// LLDP + CDP + FDB Links
	var links []protocol.LinkObservation
	locPorts := make(map[int]string)
	for oid, pdu := range lldpLocIDs {
		if p := lastIntSuffix(oid, oidLLDPLocPortID); p > 0 {
			locPorts[p] = pduText(pdu)
		}
	}
	locDescs := make(map[int]string)
	for oid, pdu := range lldpLocDescs {
		if p := lastIntSuffix(oid, oidLLDPLocPortDesc); p > 0 {
			locDescs[p] = pduText(pdu)
		}
	}

	type remRow struct {
		chassis, portID, portDescr, sysName string
	}
	remRows := make(map[string]*remRow)
	ensureRem := func(suffix string) *remRow {
		if r, ok := remRows[suffix]; ok {
			return r
		}
		r := &remRow{}
		remRows[suffix] = r
		return r
	}
	for oid, pdu := range lldpRemChassis {
		suf := strings.TrimPrefix(oid, oidLLDPRemChassis+".")
		if m := pduMAC(pdu); m != "" {
			ensureRem(suf).chassis = m
		} else {
			ensureRem(suf).chassis = pduText(pdu)
		}
	}
	for oid, pdu := range lldpRemPortIDs {
		suf := strings.TrimPrefix(oid, oidLLDPRemPortID+".")
		if m := pduMAC(pdu); m != "" {
			ensureRem(suf).portID = m
		} else {
			ensureRem(suf).portID = pduText(pdu)
		}
	}
	for oid, pdu := range lldpRemPortDescs {
		suf := strings.TrimPrefix(oid, oidLLDPRemPortDesc+".")
		ensureRem(suf).portDescr = pduText(pdu)
	}
	for oid, pdu := range lldpRemSysNames {
		suf := strings.TrimPrefix(oid, oidLLDPRemSysName+".")
		ensureRem(suf).sysName = pduText(pdu)
	}

	lldpSeenPort := make(map[string]bool)
	for suf, row := range remRows {
		parts := strings.Split(suf, ".")
		var lpNum int
		if len(parts) >= 2 {
			lpNum, _ = strconv.Atoi(parts[1])
		} else if len(parts) == 1 {
			lpNum, _ = strconv.Atoi(parts[0])
		}
		lpRaw := locPorts[lpNum]
		if lpRaw == "" && lpNum > 0 {
			lpRaw = fmt.Sprintf("Port %d", lpNum)
		}
		localPort := pickPortName(lpRaw, "", locDescs[lpNum], identity.PortCandidates(lpRaw, "", locDescs[lpNum]), job.portNames)
		if localPort == "" {
			localPort = lpRaw
		}
		lldpSeenPort[localPort] = true
		links = append(links, protocol.LinkObservation{
			ObsKey:               fmt.Sprintf("snmp-lldp:%s:%s", job.ip, suf),
			Site:                 job.site,
			Method:               "lldp",
			LocalObsKey:          job.ip,
			LocalDeviceName:      devName,
			LocalMAC:             baseMAC,
			LocalIP:              job.ip,
			LocalPort:            localPort,
			LocalPortCandidates:  identity.PortCandidates(lpRaw, "", locDescs[lpNum]),
			RemoteDeviceName:     row.sysName,
			RemoteChassisID:      row.chassis,
			RemotePort:           row.portID,
			RemotePortDescr:      row.portDescr,
			RemotePortCandidates: identity.PortCandidates(row.portID, row.portDescr, ""),
			Description:          "SNMP LLDP neighbor",
		})
	}

	// CDP entries
	for oid, pdu := range cdpDevIDs {
		suf := strings.TrimPrefix(oid, oidCDPCacheDevID+".")
		parts := strings.Split(suf, ".")
		if len(parts) == 0 {
			continue
		}
		ifIdx, _ := strconv.Atoi(parts[0])
		localPort := ifNameByIdx[ifIdx]
		if localPort == "" || lldpSeenPort[localPort] {
			continue
		}
		remPort := ""
		if pPdu, ok := cdpPorts[oidCDPCachePort+"."+suf]; ok {
			remPort = pduText(pPdu)
		}
		links = append(links, protocol.LinkObservation{
			ObsKey:           fmt.Sprintf("snmp-cdp:%s:%s", job.ip, suf),
			Site:             job.site,
			Method:           "cdp",
			LocalObsKey:      job.ip,
			LocalDeviceName:  devName,
			LocalMAC:         baseMAC,
			LocalIP:          job.ip,
			LocalPort:        localPort,
			RemoteDeviceName: pduText(pdu),
			RemotePort:       remPort,
			Description:      "SNMP CDP neighbor",
		})
	}

	// FDB access-port and LACP LAG inference (BRIDGE-MIB + Q-BRIDGE-MIB)
	bpFdbMACs := make(map[int]map[string]bool)
	macBPs := make(map[string]map[int]bool)
	recordFDB := func(bp int, mac string) {
		if bp <= 0 || mac == "" || identity.IsInvalidMAC(mac) || switchMACs[mac] {
			return
		}
		if _, ok := portIfIdx[bp]; !ok {
			return
		}
		if bpFdbMACs[bp] == nil {
			bpFdbMACs[bp] = make(map[string]bool)
		}
		bpFdbMACs[bp][mac] = true
		if macBPs[mac] == nil {
			macBPs[mac] = make(map[int]bool)
		}
		macBPs[mac][bp] = true
	}
	for oid, macPdu := range fdbAddrs {
		suf := strings.TrimPrefix(oid, oidFdbAddress+".")
		bpPdu, ok := fdbPorts[oidFdbPort+"."+suf]
		if !ok {
			continue
		}
		recordFDB(pduInt(bpPdu), pduMAC(macPdu))
	}
	for oid, bpPdu := range qFdbPorts {
		recordFDB(pduInt(bpPdu), ParseQBridgeMAC(oid))
	}

	for bp, macs := range bpFdbMACs {
		if len(macs) != 1 {
			continue
		}
		var singleMAC string
		for m := range macs {
			singleMAC = m
		}
		if len(macBPs[singleMAC]) != 1 {
			continue
		}
		idx := portIfIdx[bp]
		if mems := lagMembers[idx]; len(mems) > 0 {
			lagName := fmt.Sprintf("agg-%d", idx)
			if r := ifMap[idx]; r != nil && r.name != "" {
				lagName = r.name
			}
			for _, memIdx := range mems {
				memPort := ifNameByIdx[memIdx]
				if memPort == "" || lldpSeenPort[memPort] {
					continue
				}
				memSpeed := 0
				if r := ifMap[memIdx]; r != nil {
					memSpeed = r.speed
				}
				links = append(links, protocol.LinkObservation{
					ObsKey:          fmt.Sprintf("snmp-fdb:%s:%s->%s", job.ip, memPort, singleMAC),
					Site:            job.site,
					Method:          "snmp_fdb",
					LocalObsKey:     job.ip,
					LocalDeviceName: devName,
					LocalMAC:        baseMAC,
					LocalIP:         job.ip,
					LocalPort:       memPort,
					RemoteMAC:       singleMAC,
					CableType:       cableTypeFromSpeed(memSpeed),
					SpeedMbps:       memSpeed,
					Description:     fmt.Sprintf("Inferred via SNMP FDB (LAG %s)", lagName),
				})
			}
			continue
		}
		localPort := ifNameByIdx[idx]
		if localPort == "" || lldpSeenPort[localPort] {
			continue
		}
		pSpeed := 0
		if r := ifMap[idx]; r != nil {
			pSpeed = r.speed
		}
		links = append(links, protocol.LinkObservation{
			ObsKey:          fmt.Sprintf("snmp-fdb:%s:%s->%s", job.ip, localPort, singleMAC),
			Site:            job.site,
			Method:          "snmp_fdb",
			LocalObsKey:     job.ip,
			LocalDeviceName: devName,
			LocalMAC:        baseMAC,
			LocalIP:         job.ip,
			LocalPort:       localPort,
			RemoteMAC:       singleMAC,
			CableType:       cableTypeFromSpeed(pSpeed),
			SpeedMbps:       pSpeed,
			Description:     "Inferred via SNMP FDB",
		})
	}

	return &pollResult{Device: dev, Links: links, VLANs: vlans}, nil
}

func pickPortName(name, alias, descr string, candidates []string, mode string) string {
	if strings.EqualFold(strings.TrimSpace(name), "mgmt0") {
		return "mgmt0"
	}
	if mode == "auto" || mode == "port-number" {
		for _, c := range candidates {
			if m := rePortNumber.FindStringSubmatch(c); m != nil {
				n, _ := strconv.Atoi(m[1])
				return fmt.Sprintf("Port %d", n)
			}
		}
	}
	if mode == "port-number" {
		return ""
	}
	for _, v := range []string{name, alias, descr} {
		v = strings.TrimSpace(v)
		if v != "" && (mode == "ifname" || !reVirtualIf.MatchString(v)) {
			return v
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

func inferSNMPIfType(name, descr, alias string, ifType, speed int) string {
	txt := strings.ToLower(name + " " + descr + " " + alias)
	if strings.Contains(txt, "sfp") || strings.Contains(txt, "fiber") {
		if speed >= 10000 {
			return "10gbase-x-sfpp"
		}
		return "1000base-x-sfp"
	}
	if ifType != 6 && ifType != 0 {
		return "other"
	}
	switch {
	case speed >= 10000:
		return "10gbase-x-sfpp"
	case speed >= 5000:
		return "5gbase-t"
	case speed >= 2500:
		return "2.5gbase-t"
	case speed >= 1000:
		return "1000base-t"
	case speed >= 100:
		return "100base-tx"
	default:
		return "1000base-t"
	}
}

func inferSNMPVendor(sysObjID, sysDescr, baseMAC string) string {
	if strings.Contains(sysObjID, ".41112") || strings.Contains(sysObjID, ".4413") || strings.Contains(strings.ToLower(sysDescr), "unifi") {
		return "Ubiquiti"
	}
	if strings.Contains(sysObjID, ".890") || strings.Contains(strings.ToLower(sysDescr), "zyxel") {
		return "Zyxel"
	}
	if strings.Contains(sysObjID, ".9.1") || strings.Contains(strings.ToLower(sysDescr), "cisco") {
		return "Cisco"
	}
	if strings.Contains(sysObjID, ".14988") || strings.Contains(strings.ToLower(sysDescr), "mikrotik") {
		return "MikroTik"
	}
	if strings.Contains(sysObjID, ".318") || strings.Contains(strings.ToLower(sysDescr), "apc") {
		return "APC"
	}
	if strings.Contains(sysObjID, ".674") {
		return "Dell"
	}
	if baseMAC != "" {
		return identity.LookupVendor(baseMAC)
	}
	return ""
}

func inferSNMPRole(sysServices int, sysDescr, hintRole string) string {
	if hintRole != "" {
		return hintRole
	}
	lower := strings.ToLower(sysDescr)
	switch {
	case strings.Contains(lower, "udm") || strings.Contains(lower, "router") || strings.Contains(lower, "gateway"):
		return "gateway"
	case strings.Contains(lower, "uap") || strings.Contains(lower, "access point"):
		return "access-point"
	case strings.Contains(lower, "apc") || strings.Contains(lower, "pdu") || strings.Contains(lower, "ups"):
		return "power"
	case (sysServices & 4) != 0:
		return "gateway"
	case (sysServices & 2) != 0:
		return "switch"
	default:
		return "switch"
	}
}

func parseBitmap(data []byte) map[int]bool {
	out := make(map[int]bool)
	for byteIdx, b := range data {
		for bit := 0; bit < 8; bit++ {
			if (b & (0x80 >> bit)) != 0 {
				out[byteIdx*8+bit+1] = true
			}
		}
	}
	return out
}

func ParseQBridgeMAC(oid string) string {
	prefix := strings.TrimPrefix(oidQBridgeFdbPort, ".") + "."
	rest := strings.TrimPrefix(strings.TrimPrefix(oid, "."), prefix)
	if rest == "" {
		return ""
	}
	parts := strings.Split(rest, ".")
	if len(parts) < 7 {
		return ""
	}
	macParts := parts[len(parts)-6:]
	var b [6]byte
	for i, p := range macParts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || v > 255 {
			return ""
		}
		b[i] = byte(v)
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
}

func lastCiscoVlanSuffix(oid, base string) int {
	prefix := strings.TrimPrefix(base, ".") + "."
	rest := strings.TrimPrefix(strings.TrimPrefix(oid, "."), prefix)
	if rest == "" {
		return 0
	}
	parts := strings.Split(rest, ".")
	if len(parts) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(parts[len(parts)-1])
	return n
}

func cableTypeFromSpeed(speed int) string {
	switch {
	case speed >= 10000:
		return "dac-passive"
	case speed >= 2500:
		return "cat6a"
	default:
		return "cat6"
	}
}

func isExpectedSNMPUnavailable(err error) bool {
	if err == nil {
		return false
	}
	l := strings.ToLower(err.Error())
	return strings.Contains(l, "connection refused") ||
		strings.Contains(l, "timeout") ||
		strings.Contains(l, "deadline exceeded")
}

func lastIntSuffix(oid, base string) int {
	prefix := strings.TrimPrefix(base, ".") + "."
	rest := strings.TrimPrefix(strings.TrimPrefix(oid, "."), prefix)
	if rest == "" || strings.Contains(rest, ".") {
		return 0
	}
	n, _ := strconv.Atoi(rest)
	return n
}

func pduText(pdu gosnmp.SnmpPDU) string {
	switch v := pdu.Value.(type) {
	case []byte:
		return strings.TrimSpace(strings.Trim(string(v), "\"\x00"))
	case string:
		return strings.TrimSpace(strings.Trim(v, "\"\x00"))
	case int:
		return strconv.Itoa(v)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func pduInt(pdu gosnmp.SnmpPDU) int {
	n := gosnmp.ToBigInt(pdu.Value)
	if n != nil {
		return int(n.Int64())
	}
	v, _ := strconv.Atoi(pduText(pdu))
	return v
}

func pduBytes(pdu gosnmp.SnmpPDU) []byte {
	if b, ok := pdu.Value.([]byte); ok {
		return b
	}
	return nil
}

func pduMAC(pdu gosnmp.SnmpPDU) string {
	if b, ok := pdu.Value.([]byte); ok && len(b) == 6 {
		return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
	}
	if m, err := identity.NormalizeMAC(pduText(pdu)); err == nil {
		return m
	}
	return ""
}

func firstNonEmptyVal(m map[string]gosnmp.SnmpPDU) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if t := pduText(m[k]); t != "" {
			return t
		}
	}
	return ""
}
