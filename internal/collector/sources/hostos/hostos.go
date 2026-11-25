package hostos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blalange/auto-netviz/internal/collector/config"
	"github.com/blalange/auto-netviz/internal/collector/sources"
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/protocol"
	"golang.org/x/crypto/ssh"
)

type Source struct {
	Profiles []config.HostOSProfile
}

func (s *Source) Name() string { return "hostos" }

func (s *Source) Enabled() bool {
	for _, p := range s.Profiles {
		if config.IsEnabled(p.Enabled) {
			return true
		}
	}
	return false
}

const remoteProbeScript = `
echo "===HOSTNAME==="; hostname -f 2>/dev/null || hostname
echo "===DMI==="
for k in sys_vendor product_name product_serial product_uuid; do
  echo "$k=$(cat /sys/class/dmi/id/$k 2>/dev/null || true)"
done
echo "===OS==="
cat /etc/os-release 2>/dev/null || true
echo "===UNAME==="
uname -r
echo "===CPU==="
lscpu -J 2>/dev/null || true
echo "===LSBLK==="
lsblk -J -b -o NAME,TYPE,SIZE,MODEL,SERIAL,ROTA,TRAN 2>/dev/null || true
echo "===IPADDR==="
ip -j addr show 2>/dev/null || true
`

func (s *Source) Collect(ctx context.Context, rt sources.Runtime) ([]protocol.ObservationBatch, []protocol.SourceHealthPayload, error) {
	var batches []protocol.ObservationBatch
	var healths []protocol.SourceHealthPayload

	for _, p := range s.Profiles {
		if !config.IsEnabled(p.Enabled) {
			continue
		}
		start := time.Now()
		site := p.Site
		if site == "" {
			site = rt.Site
		}
		h := protocol.SourceHealthPayload{Source: "hostos", Profile: p.Name, Enabled: true, LastRunAt: &start}

		var devs []protocol.DeviceObservation
		var errs []string

		if p.Local {
			obs, err := collectLocalHost(ctx, site)
			if err != nil {
				errs = append(errs, "local: "+err.Error())
			} else {
				devs = append(devs, obs)
			}
		}

		// Collect SSH targets configured on profile + matching hints
		seenHosts := make(map[string]bool)
		var sshJobs []config.SSHTarget
		for _, t := range p.Targets {
			if t.Host != "" && !seenHosts[t.Host] {
				seenHosts[t.Host] = true
				sshJobs = append(sshJobs, t)
			}
		}
		for _, hint := range rt.Hints {
			if hint.SSHHost == "" || seenHosts[hint.SSHHost] {
				continue
			}
			if hint.SSHCredential != "" && hint.SSHCredential != p.Name {
				continue
			}
			if hint.SSHCredential == "" && p.Match != nil && !p.Match.Matches(hint.Site, hint.DeviceName, hint.Role, hint.Tags, hint.SSHHost) && !p.Default {
				continue
			}
			seenHosts[hint.SSHHost] = true
			sshJobs = append(sshJobs, config.SSHTarget{
				Name:                  hint.DeviceName,
				Host:                  hint.SSHHost,
				Port:                  22,
				Site:                  hint.Site,
				User:                  p.User,
				Password:              p.Password,
				PrivateKeyPath:        p.KeyPath,
				InsecureIgnoreHostKey: true,
			})
		}

		for _, job := range sshJobs {
			tSite := job.Site
			if tSite == "" {
				tSite = site
			}
			obs, err := collectSSHHost(ctx, rt.BaseDir, rt.K8s, job, p, tSite)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", job.Host, err))
				continue
			}
			devs = append(devs, obs)
		}

		batches = append(batches, protocol.ObservationBatch{
			BatchID:      fmt.Sprintf("hostos-%s-%d", p.Name, time.Now().UnixNano()),
			CollectorID:  rt.CollectorID,
			Site:         site,
			Source:       "hostos",
			Profile:      p.Name,
			CollectedAt:  time.Now().UTC(),
			TTLSeconds:   600,
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
				h.Error = errs[0]
			}
		}
		healths = append(healths, h)
	}
	return batches, healths, nil
}

func collectLocalHost(ctx context.Context, site string) (protocol.DeviceObservation, error) {
	hostname, _ := os.Hostname()
	readDMI := func(name string) string {
		b, err := os.ReadFile(filepath.Join("/sys/class/dmi/id", name))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	osRelease, _ := os.ReadFile("/etc/os-release")
	unameOut, _ := exec.CommandContext(ctx, "uname", "-r").Output()
	lscpuOut, _ := exec.CommandContext(ctx, "lscpu", "-J").Output()
	lsblkOut, _ := exec.CommandContext(ctx, "lsblk", "-J", "-b", "-o", "NAME,TYPE,SIZE,MODEL,SERIAL,ROTA,TRAN").Output()
	ipOut, _ := exec.CommandContext(ctx, "ip", "-j", "addr", "show").Output()

	return ParseHostProbeOutput(
		site,
		hostname,
		hostname,
		readDMI("sys_vendor"),
		readDMI("product_name"),
		readDMI("product_serial"),
		readDMI("product_uuid"),
		string(osRelease),
		strings.TrimSpace(string(unameOut)),
		lscpuOut,
		lsblkOut,
		ipOut,
	), nil
}

func collectSSHHost(ctx context.Context, baseDir string, k8s *config.K8sResolver, job config.SSHTarget, prof config.HostOSProfile, site string) (protocol.DeviceObservation, error) {
	user := job.User
	if user == "" {
		user = prof.User
	}
	if user == "" {
		user = "root"
	}
	keyPath := job.PrivateKeyPath
	if keyPath == "" {
		keyPath = prof.KeyPath
	}

	var authMethods []ssh.AuthMethod
	if keyPath != "" {
		if !filepath.IsAbs(keyPath) && baseDir != "" {
			keyPath = filepath.Join(baseDir, keyPath)
		}
		if keyBytes, err := os.ReadFile(keyPath); err == nil {
			if signer, err := ssh.ParsePrivateKey(keyBytes); err == nil {
				authMethods = append(authMethods, ssh.PublicKeys(signer))
			}
		}
	}
	pass, _ := job.Password.Resolve(ctx, baseDir, k8s, "")
	if pass == "" {
		pass, _ = prof.Password.Resolve(ctx, baseDir, k8s, "")
	}
	if pass != "" {
		authMethods = append(authMethods, ssh.Password(pass))
	}

	port := job.Port
	if port <= 0 {
		port = 22
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		Timeout:         10 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
	}

	conn, err := ssh.Dial("tcp", net.JoinHostPort(job.Host, strconv.Itoa(port)), cfg)
	if err != nil {
		return protocol.DeviceObservation{}, err
	}
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		return protocol.DeviceObservation{}, err
	}
	defer sess.Close()

	var stdout bytes.Buffer
	sess.Stdout = &stdout
	if err := sess.Run(remoteProbeScript); err != nil {
		return protocol.DeviceObservation{}, err
	}

	sections := splitSections(stdout.String())
	dmi := parseKV(sections["DMI"])
	host := strings.TrimSpace(sections["HOSTNAME"])
	if host == "" {
		host = job.Name
	}
	obs := ParseHostProbeOutput(
		site,
		job.Host,
		host,
		dmi["sys_vendor"],
		dmi["product_name"],
		dmi["product_serial"],
		dmi["product_uuid"],
		sections["OS"],
		strings.TrimSpace(sections["UNAME"]),
		[]byte(sections["CPU"]),
		[]byte(sections["LSBLK"]),
		[]byte(sections["IPADDR"]),
	)
	if obs.PrimaryIP == "" {
		obs.PrimaryIP = identity.NormalizeIP(job.Host)
	}
	obs.Claims.Locators["ssh_host"] = job.Host
	return obs, nil
}

func ParseHostProbeOutput(
	site, obsKey, hostname, vendor, model, rawSerial, rawUUID, osRelease, kernel string,
	lscpuJSON, lsblkJSON, ipAddrJSON []byte,
) protocol.DeviceObservation {
	serial := identity.NormalizeSerial(rawSerial)
	sysUUID := identity.NormalizeUUID(rawUUID)
	osPretty := parseOSPrettyName(osRelease)

	var modules []protocol.ModuleObservation
	// Parse lscpu JSON
	if len(lscpuJSON) > 0 {
		var cpuDoc struct {
			Lscpu []struct {
				Field string `json:"field"`
				Data  string `json:"data"`
			} `json:"lscpu"`
		}
		if json.Unmarshal(lscpuJSON, &cpuDoc) == nil {
			var cpuModel string
			var cores int
			for _, item := range cpuDoc.Lscpu {
				f := strings.ToLower(item.Field)
				if strings.HasPrefix(f, "model name") {
					cpuModel = strings.TrimSpace(item.Data)
				} else if strings.HasPrefix(f, "cpu(s):") {
					cores, _ = strconv.Atoi(strings.TrimSpace(item.Data))
				}
			}
			if cpuModel != "" {
				modules = append(modules, protocol.ModuleObservation{
					Bay:        "CPU 1",
					BayType:    "cpu-socket",
					Profile:    "CPU",
					Model:      cpuModel,
					Status:     "active",
					Attributes: map[string]any{"cores": cores},
				})
			}
		}
	}

	// Parse lsblk JSON
	if len(lsblkJSON) > 0 {
		var blkDoc struct {
			BlockDevices []struct {
				Name   string `json:"name"`
				Type   string `json:"type"`
				Size   int64  `json:"size"`
				Model  string `json:"model"`
				Serial string `json:"serial"`
				Rota   bool   `json:"rota"`
				Tran   string `json:"tran"`
			} `json:"blockdevices"`
		}
		if json.Unmarshal(lsblkJSON, &blkDoc) == nil {
			driveNum := 0
			for _, b := range blkDoc.BlockDevices {
				if b.Type != "disk" || b.Size <= 0 {
					continue
				}
				driveNum++
				sizeGB := int(b.Size / 1_000_000_000)
				kind := "SSD"
				if strings.EqualFold(b.Tran, "nvme") {
					kind = "NVME"
				} else if b.Rota {
					kind = "HD"
				}
				modules = append(modules, protocol.ModuleObservation{
					Bay:         fmt.Sprintf("Drive %d", driveNum),
					BayType:     "drive-2-5",
					Profile:     "Hard disk",
					Model:       strings.TrimSpace(b.Model),
					Serial:      identity.NormalizeSerial(b.Serial),
					Status:      "active",
					Description: fmt.Sprintf("%d GB %s (%s)", sizeGB, kind, b.Name),
					Attributes:  map[string]any{"size_gb": sizeGB, "type": kind},
				})
			}
		}
	}

	// Parse ip -j addr show
	var ifaces []protocol.InterfaceObservation
	var macs []string
	var primaryIP string
	if len(ipAddrJSON) > 0 {
		var ipDevs []struct {
			IfName    string `json:"ifname"`
			LinkType  string `json:"link_type"`
			OperState string `json:"operstate"`
			MTU       int    `json:"mtu"`
			Address   string `json:"address"`
			AddrInfo  []struct {
				Family    string `json:"family"`
				Local     string `json:"local"`
				PrefixLen int    `json:"prefixlen"`
				Scope     string `json:"scope"`
			} `json:"addr_info"`
		}
		if json.Unmarshal(ipAddrJSON, &ipDevs) == nil {
			for _, d := range ipDevs {
				if d.IfName == "lo" || strings.HasPrefix(d.IfName, "veth") || strings.HasPrefix(d.IfName, "docker") || strings.HasPrefix(d.IfName, "br-") || strings.HasPrefix(d.IfName, "cilium") || strings.HasPrefix(d.IfName, "cni") {
					continue
				}
				mac, _ := identity.NormalizeMAC(d.Address)
				if mac != "" && !identity.IsInvalidMAC(mac) && !identity.IsLocallyAdministeredMAC(mac) {
					macs = append(macs, mac)
				}
				var ips []protocol.IPObservation
				for _, a := range d.AddrInfo {
					if a.Scope == "link" || a.Scope == "host" {
						continue
					}
					if a.Family != "inet" {
						continue
					}
					cidr := fmt.Sprintf("%s/%d", a.Local, a.PrefixLen)
					isPrimary := primaryIP == ""
					if isPrimary {
						primaryIP = a.Local
					}
					ips = append(ips, protocol.IPObservation{
						Address: cidr, DNSName: hostname, Primary: isPrimary,
					})
				}
				up := strings.EqualFold(d.OperState, "UP")
				ifaces = append(ifaces, protocol.InterfaceObservation{
					Name:        d.IfName,
					Type:        "1000base-t",
					Kind:        "physical",
					LinkUp:      &up,
					MTU:         d.MTU,
					MAC:         mac,
					IPAddresses: ips,
				})
			}
		}
	}

	claims := protocol.IdentityClaims{
		MACs:      macs,
		Hostnames: []string{hostname},
		Locators:  map[string]string{},
	}
	if sysUUID != "" {
		claims.UUIDs = []string{sysUUID}
	}
	if serial != "" {
		claims.Serials = []string{serial}
	}
	if primaryIP != "" {
		claims.Locators["mgmt_ip"] = primaryIP
	}

	return protocol.DeviceObservation{
		ObsKey:        obsKey,
		Site:          site,
		Name:          hostname,
		RoleHint:      "server",
		Manufacturer:  strings.TrimSpace(vendor),
		Model:         strings.TrimSpace(model),
		Status:        "active",
		Serial:        serial,
		SystemUUID:    sysUUID,
		OSVersion:     osPretty,
		KernelVersion: kernel,
		PrimaryIP:     primaryIP,
		Claims:        claims,
		Interfaces:    ifaces,
		Modules:       modules,
	}
}

func splitSections(raw string) map[string]string {
	res := make(map[string]string)
	var curKey string
	var buf strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "===") && strings.HasSuffix(line, "===") && len(line) > 6 {
			if curKey != "" {
				res[curKey] = buf.String()
			}
			curKey = strings.Trim(line, "=")
			buf.Reset()
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	if curKey != "" {
		res[curKey] = buf.String()
	}
	return res
}

func parseKV(raw string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func parseOSPrettyName(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"'")
		}
	}
	return ""
}
