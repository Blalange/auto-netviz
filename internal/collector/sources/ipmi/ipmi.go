package ipmi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blalange/auto-netviz/internal/collector/config"
	"github.com/blalange/auto-netviz/internal/collector/sources"
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/protocol"
)

var (
	rePSUFru   = regexp.MustCompile(`(?i)^(?:PS|PSU|POWER\s*SUPPLY)\s*(\d+)\b`)
	reWattage  = regexp.MustCompile(`(?i)(\d+)\s*W\b`)
	reRPM      = regexp.MustCompile(`(?i)(\d+)\s*RPM`)
	rePSUSDR   = regexp.MustCompile(`(?i)^(?:PSU|PS|PWR|POWER\s*SUPPLY)\s*(\d+)(?:\s+(.*))?$`)
	reNumber   = regexp.MustCompile(`(\d+)`)
	reFanSuffix = regexp.MustCompile(`(?i)\s+RPM$`)
)

type Source struct {
	Profiles []config.IPMIProfile
}

func (s *Source) Name() string { return "ipmi" }

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
	profile              config.IPMIProfile
}

func (s *Source) Collect(ctx context.Context, rt sources.Runtime) ([]protocol.ObservationBatch, []protocol.SourceHealthPayload, error) {
	seen := make(map[string]bool)
	var jobs []target

	add := func(ip, name, site, role string, tags []string, explicit string, override *config.IPMIProfile) {
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
		var prof *config.IPMIProfile
		var err error
		if override != nil {
			prof = override
		} else {
			prof, err = config.MatchIPMIProfile(s.Profiles, explicit, site, name, role, tags, ip)
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
		if h.IPMIIP != "" {
			add(h.IPMIIP, h.DeviceName, h.Site, h.Role, h.Tags, h.IPMICredential, nil)
		}
	}
	if rt.Shared != nil {
		for _, pr := range rt.Shared.Probed() {
			if pr.HasIPMI {
				add(pr.IP, "", pr.Site, "server", nil, "", nil)
			}
		}
	}

	if len(jobs) == 0 {
		return nil, []protocol.SourceHealthPayload{{
			Source: "ipmi", Profile: "default", Enabled: true, Status: "ok",
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
		h := protocol.SourceHealthPayload{Source: "ipmi", Profile: profName, Enabled: true, LastRunAt: &start}
		bin := prof.Binary
		if bin == "" {
			bin = "ipmitool"
		}
		if _, err := exec.LookPath(bin); err != nil {
			h.Status = "error"
			h.Error = fmt.Sprintf("%s binary not found", bin)
			h.DurationMs = time.Since(start).Milliseconds()
			healths = append(healths, h)
			continue
		}
		user, errU := prof.Username.Resolve(ctx, rt.BaseDir, rt.K8s, "")
		pass, errP := prof.Password.Resolve(ctx, rt.BaseDir, rt.K8s, "")
		if errU != nil || errP != nil || user == "" || pass == "" {
			h.Status = "error"
			h.Error = "missing IPMI credentials"
			h.DurationMs = time.Since(start).Milliseconds()
			healths = append(healths, h)
			continue
		}

		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		var devs []protocol.DeviceObservation
		var errs []string

		for _, job := range pJobs {
			wg.Add(1)
			go func(j target) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				obs, err := readIPMI(ctx, bin, j, user, pass)
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
			BatchID:      fmt.Sprintf("ipmi-%s-%d", profName, time.Now().UnixNano()),
			CollectorID:  rt.CollectorID,
			Site:         rt.Site,
			Source:       "ipmi",
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
		}
		healths = append(healths, h)
	}
	return batches, healths, nil
}

func runIPMITool(ctx context.Context, bin, ip, user, pass string, prof config.IPMIProfile, args ...string) (string, error) {
	iface := prof.Interface
	if iface == "" {
		iface = "lanplus"
	}
	priv := prof.Privilege
	if priv == "" {
		priv = "USER"
	}
	cmdArgs := []string{"-I", iface, "-H", ip, "-U", user, "-E", "-L", priv, "-N", "3", "-R", "1"}
	if prof.Cipher != "" {
		cmdArgs = append(cmdArgs, "-C", prof.Cipher)
	}
	cmdArgs = append(cmdArgs, args...)

	cctx, cancel := context.WithTimeout(ctx, config.ParseDuration(prof.Timeout, 15*time.Second))
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, cmdArgs...)
	cmd.Env = append(os.Environ(), "IPMI_PASSWORD="+pass)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ipmitool %v: %v (%s)", args, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func readIPMI(ctx context.Context, bin string, job target, user, pass string) (protocol.DeviceObservation, error) {
	fruOut, fruErr := runIPMITool(ctx, bin, job.ip, user, pass, job.profile, "fru", "print")
	sdrOut, sdrErr := runIPMITool(ctx, bin, job.ip, user, pass, job.profile, "sdr")
	lanOut, _ := runIPMITool(ctx, bin, job.ip, user, pass, job.profile, "lan", "print")
	pwrOut, _ := runIPMITool(ctx, bin, job.ip, user, pass, job.profile, "chassis", "power", "status")
	if fruErr != nil && sdrErr != nil {
		return protocol.DeviceObservation{}, fruErr
	}

	mfg, model, serial := ParseFRU(fruOut)
	modules, sensors := ParseSDR(sdrOut, mfg)
	psuFrus := ParseFRUPSUs(fruOut, mfg)
	modByBay := make(map[string]protocol.ModuleObservation)
	for _, m := range modules {
		modByBay[m.Bay] = m
	}
	for _, p := range psuFrus {
		modByBay[p.Bay] = p
	}
	modules = modules[:0]
	for _, m := range modByBay {
		modules = append(modules, m)
	}

	bmcMAC := parseLANMAC(lanOut)
	powerState := "unknown"
	if strings.Contains(strings.ToLower(pwrOut), "is on") {
		powerState = "on"
	} else if strings.Contains(strings.ToLower(pwrOut), "is off") {
		powerState = "off"
	}

	claims := protocol.IdentityClaims{
		Locators: map[string]string{"ipmi_ip": job.ip, "oob_ip": job.ip},
	}
	if serial != "" {
		claims.Serials = []string{serial}
	}
	if bmcMAC != "" {
		claims.MACs = []string{bmcMAC}
	}
	if job.name != "" {
		claims.Hostnames = []string{job.name}
	}

	var ifaces []protocol.InterfaceObservation
	ifaces = append(ifaces, protocol.InterfaceObservation{
		Name:        "iDRAC",
		Type:        "1000base-t",
		Kind:        "oob",
		MgmtOnly:    true,
		MAC:         bmcMAC,
		IPAddresses: []protocol.IPObservation{{Address: job.ip + "/24"}},
	})

	status := "active"
	if powerState == "off" {
		status = "offline"
	}

	return protocol.DeviceObservation{
		ObsKey:       job.ip,
		Site:         job.site,
		Name:         job.name,
		RoleHint:     "server",
		Manufacturer: mfg,
		Model:        model,
		Status:       status,
		Serial:       serial,
		ServiceTag:   serial,
		PowerState:   powerState,
		OOBIP:        job.ip,
		Claims:       claims,
		Interfaces:   ifaces,
		Modules:      modules,
		Sensors:      sensors,
	}, nil
}

func ParseFRU(text string) (manufacturer, model, serial string) {
	frus := splitFRUDevices(text)
	if len(frus) == 0 {
		return "", "", ""
	}
	f := frus[0]
	manufacturer = firstNonEmpty(f["Product Manufacturer"], f["Board Mfg"])
	prod := strings.TrimSpace(f["Product Name"])
	if len(prod) > 2 {
		model = prod
	} else {
		model = f["Board Product"]
	}
	serial = identity.NormalizeSerial(firstNonEmpty(f["Product Serial"], f["Board Serial"]))
	return normalizeVendor(manufacturer), model, serial
}

func ParseFRUPSUs(text, vendor string) []protocol.ModuleObservation {
	if vendor == "" {
		vendor = "Generic"
	}
	var out []protocol.ModuleObservation
	for i, f := range splitFRUDevices(text) {
		desc := f["FRU Device Description"]
		prod := strings.TrimSpace(f["Board Product"])
		hay := strings.ToLower(desc + " " + prod)
		if !rePSUFru.MatchString(desc) && !strings.Contains(hay, "pwr spl") && !strings.Contains(hay, "power supply") {
			continue
		}
		bay := fmt.Sprintf("PSU %d", i+1)
		if m := reNumber.FindStringSubmatch(desc); m != nil {
			bay = "PSU " + m[1]
		}
		watts := 0
		if m := reWattage.FindStringSubmatch(prod); m != nil {
			watts, _ = strconv.Atoi(m[1])
		}
		mfg := normalizeVendor(f["Board Mfg"])
		if mfg == "" {
			mfg = vendor
		}
		if prod == "" {
			prod = "Power Supply"
		}
		out = append(out, protocol.ModuleObservation{
			Bay:          bay,
			BayType:      "power-supply",
			Profile:      "Power supply",
			Manufacturer: mfg,
			Model:        prod,
			Serial:       identity.NormalizeSerial(f["Board Serial"]),
			Status:       "active",
			Description:  fmt.Sprintf("%d W", watts),
			Attributes:   map[string]any{"wattage": watts},
		})
	}
	return out
}

func ParseSDR(text, vendor string) ([]protocol.ModuleObservation, []protocol.SensorObservation) {
	if vendor == "" {
		vendor = "Generic"
	}
	var mods []protocol.ModuleObservation
	var sensors []protocol.SensorObservation
	seen := make(map[string]bool)

	for _, line := range strings.Split(text, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		statusRaw := ""
		if len(parts) > 2 {
			statusRaw = strings.TrimSpace(parts[2])
		}
		status := sensorStatus(statusRaw)

		if m := reRPM.FindStringSubmatch(val); m != nil && strings.Contains(strings.ToLower(name), "fan") {
			rpm, _ := strconv.Atoi(m[1])
			bay := strings.TrimSpace(reFanSuffix.ReplaceAllString(name, ""))
			if bay == "" {
				bay = name
			}
			if !seen[bay] {
				seen[bay] = true
				mods = append(mods, protocol.ModuleObservation{
					Bay: bay, BayType: "fan", Profile: "Fan", Manufacturer: vendor,
					Model: "System Board Fan", Status: status, Description: name,
					Attributes: map[string]any{"rpm": rpm},
				})
				sensors = append(sensors, protocol.SensorObservation{
					Name: name, Kind: "fan", Value: float64(rpm), Unit: "RPM", Status: status,
				})
			}
			continue
		}
		if m := rePSUSDR.FindStringSubmatch(name); m != nil {
			bay := "PSU " + m[1]
			if !seen[bay] {
				seen[bay] = true
				mods = append(mods, protocol.ModuleObservation{
					Bay: bay, BayType: "power-supply", Profile: "Power supply",
					Manufacturer: vendor, Model: "Power Supply", Status: status,
				})
			}
		}
	}
	return mods, sensors
}

func splitFRUDevices(text string) []map[string]string {
	var res []map[string]string
	var cur map[string]string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "FRU Device Description") {
			if cur != nil {
				res = append(res, cur)
			}
			cur = make(map[string]string)
		}
		if cur == nil {
			cur = make(map[string]string)
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k != "" && v != "" && cur[k] == "" {
			cur[k] = v
		}
	}
	if len(cur) > 0 {
		res = append(res, cur)
	}
	return res
}

func parseLANMAC(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(strings.ToLower(line), "mac address") {
			_, v, ok := strings.Cut(line, ":")
			if ok {
				if norm, err := identity.NormalizeMAC(strings.TrimSpace(v)); err == nil {
					return norm
				}
			}
		}
	}
	return ""
}

func sensorStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ok":
		return "active"
	case "ns", "disabled", "not readable":
		return "inventory"
	default:
		return "failed"
	}
}

func normalizeVendor(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(l, "dell"):
		return "Dell"
	case strings.Contains(l, "hewlett"), strings.Contains(l, "hpe"):
		return "HPE"
	case strings.Contains(l, "ibm"), strings.Contains(l, "lenovo"):
		return "Lenovo"
	case strings.Contains(l, "supermicro"):
		return "Supermicro"
	default:
		return strings.TrimSpace(s)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
