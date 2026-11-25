package lldp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/blalange/auto-netviz/internal/collector/config"
	"github.com/blalange/auto-netviz/internal/collector/sources"
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/protocol"
)

type Source struct {
	Profiles []config.LLDPProfile

	mu       sync.Mutex
	listener bool
	frames   map[string]cachedFrame // key: iface|chassis|port
}

type cachedFrame struct {
	Iface      string
	Frame      LLDPFrame
	ObservedAt time.Time
}

// LLDPFrame represents a decoded IEEE 802.1AB LLDPDU payload.
type LLDPFrame struct {
	ChassisID   string
	PortID      string
	PortDescr   string
	SysName     string
	SysDescr    string
	MgmtIP      string
	RoleHint    string
	TTLSeconds  int
}

func (s *Source) Name() string { return "lldp" }

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

	localHost, _ := os.Hostname()
	if localHost == "" {
		localHost = rt.CollectorID
	}

	for _, p := range s.Profiles {
		if !config.IsEnabled(p.Enabled) {
			continue
		}
		start := time.Now()
		site := p.Site
		if site == "" {
			site = rt.Site
		}
		h := protocol.SourceHealthPayload{
			Source: "lldp", Profile: p.Name, Enabled: true, LastRunAt: &start,
		}

		if p.PassiveRaw {
			s.ensureRawSniffer(ctx, p.Interfaces, rt)
		}

		var devs []protocol.DeviceObservation
		var links []protocol.LinkObservation
		var errs []string

		// 1. Collect from local lldpctl -f json if present
		if config.IsEnabled(p.LLDPCtl) {
			if bin, err := exec.LookPath("lldpctl"); err == nil {
				cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				out, err := exec.CommandContext(cmdCtx, bin, "-f", "json").Output()
				cancel()
				if err != nil {
					errs = append(errs, fmt.Sprintf("lldpctl: %v", err))
				} else {
					d, l := parseLLDPCtlJSON(out, site, localHost)
					devs = append(devs, d...)
					links = append(links, l...)
				}
			}
		}

		// 2. Drain passive AF_PACKET LLDP frames
		if p.PassiveRaw {
			d, l := s.snapshotRawFrames(site, localHost)
			devs = append(devs, d...)
			links = append(links, l...)
		}

		batch := protocol.ObservationBatch{
			BatchID:      fmt.Sprintf("lldp-%s-%d", p.Name, time.Now().UnixNano()),
			CollectorID:  rt.CollectorID,
			Site:         site,
			Source:       "lldp",
			Profile:      p.Name,
			CollectedAt:  time.Now().UTC(),
			TTLSeconds:   300,
			FullSnapshot: true,
			Devices:      devs,
			Links:        links,
			Errors:       errs,
		}
		batches = append(batches, batch)

		done := time.Now()
		h.Status = "ok"
		if len(errs) > 0 && len(devs) == 0 {
			h.Status = "error"
			h.Error = strings.Join(errs, "; ")
		}
		h.LastSuccessAt = &done
		h.DurationMs = time.Since(start).Milliseconds()
		h.DeviceCount = len(devs)
		h.LinkCount = len(links)
		healths = append(healths, h)
	}
	return batches, healths, nil
}

// DecodeLLDPDU decodes an IEEE 802.1AB LLDP payload (after the 14-byte Ethernet header).
func DecodeLLDPDU(payload []byte) (LLDPFrame, error) {
	var frame LLDPFrame
	pos := 0
	for pos+2 <= len(payload) {
		hdr := binary.BigEndian.Uint16(payload[pos : pos+2])
		tlvType := int(hdr >> 9)
		tlvLen := int(hdr & 0x01ff)
		pos += 2
		if tlvType == 0 {
			break
		}
		if pos+tlvLen > len(payload) {
			return frame, fmt.Errorf("truncated LLDP TLV type %d", tlvType)
		}
		val := payload[pos : pos+tlvLen]
		pos += tlvLen

		switch tlvType {
		case 1: // Chassis ID
			if len(val) >= 2 {
				if val[0] == 4 && len(val) == 7 { // MAC address subtype
					frame.ChassisID = fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", val[1], val[2], val[3], val[4], val[5], val[6])
				} else {
					frame.ChassisID = strings.TrimSpace(string(val[1:]))
				}
			}
		case 2: // Port ID
			if len(val) >= 2 {
				if val[0] == 3 && len(val) == 7 { // MAC address subtype
					frame.PortID = fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", val[1], val[2], val[3], val[4], val[5], val[6])
				} else {
					frame.PortID = strings.TrimSpace(string(val[1:]))
				}
			}
		case 3: // TTL
			if len(val) >= 2 {
				frame.TTLSeconds = int(binary.BigEndian.Uint16(val[:2]))
			}
		case 4: // Port Description
			frame.PortDescr = strings.TrimSpace(string(val))
		case 5: // System Name
			frame.SysName = strings.TrimSpace(string(val))
		case 6: // System Description
			frame.SysDescr = strings.TrimSpace(string(val))
		case 7: // System Capabilities (4 bytes: 2 cap, 2 enabled)
			if len(val) >= 4 {
				enabled := binary.BigEndian.Uint16(val[2:4])
				switch {
				case (enabled & (1 << 4)) != 0:
					frame.RoleHint = "gateway"
				case (enabled & (1 << 2)) != 0:
					frame.RoleHint = "switch"
				case (enabled & (1 << 3)) != 0:
					frame.RoleHint = "access-point"
				case (enabled & (1 << 7)) != 0:
					frame.RoleHint = "server"
				}
			}
		case 8: // Management Address
			if len(val) >= 6 && val[0] >= 5 && val[1] == 1 { // IPv4 (len=5, family=1)
				frame.MgmtIP = net.IP(val[2:6]).String()
			}
		}
	}
	return frame, nil
}

func (s *Source) ensureRawSniffer(ctx context.Context, ifaces []string, rt sources.Runtime) {
	s.mu.Lock()
	if s.listener {
		s.mu.Unlock()
		return
	}
	if s.frames == nil {
		s.frames = make(map[string]cachedFrame)
	}
	// Open Linux AF_PACKET socket for EtherType 0x88cc (LLDP)
	proto := htons(0x88cc)
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(proto))
	if err != nil {
		s.mu.Unlock()
		rt.Log.Warn("passive LLDP raw socket unavailable (requires CAP_NET_RAW)", "err", err)
		return
	}
	s.listener = true
	s.mu.Unlock()

	ifMap := make(map[int]string)
	if netIfs, err := net.Interfaces(); err == nil {
		filter := make(map[string]bool)
		for _, n := range ifaces {
			filter[n] = true
		}
		for _, nif := range netIfs {
			if len(filter) == 0 || filter[nif.Name] {
				ifMap[nif.Index] = nif.Name
			}
		}
	}

	go func() {
		defer syscall.Close(fd)
		buf := make([]byte, 2048)
		for {
			if ctx.Err() != nil {
				return
			}
			_ = syscall.SetNonblock(fd, false)
			tv := syscall.Timeval{Sec: 2}
			_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)

			n, from, err := syscall.Recvfrom(fd, buf, 0)
			if err != nil || n < 18 {
				continue
			}
			llAddr, ok := from.(*syscall.SockaddrLinklayer)
			if !ok {
				continue
			}
			ifaceName := ifMap[llAddr.Ifindex]
			if ifaceName == "" {
				continue
			}
			etherType := binary.BigEndian.Uint16(buf[12:14])
			if etherType != 0x88cc {
				continue
			}
			frame, err := DecodeLLDPDU(buf[14:n])
			if err != nil || frame.ChassisID == "" {
				continue
			}
			key := ifaceName + "|" + frame.ChassisID + "|" + frame.PortID
			s.mu.Lock()
			s.frames[key] = cachedFrame{Iface: ifaceName, Frame: frame, ObservedAt: time.Now().UTC()}
			s.mu.Unlock()
		}
	}()
}

func (s *Source) snapshotRawFrames(site, localHost string) ([]protocol.DeviceObservation, []protocol.LinkObservation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().UTC().Add(-5 * time.Minute)
	var devs []protocol.DeviceObservation
	var links []protocol.LinkObservation
	for k, cf := range s.frames {
		if cf.ObservedAt.Before(cutoff) {
			delete(s.frames, k)
			continue
		}
		f := cf.Frame
		obsKey := firstStr(f.ChassisID, f.SysName, f.MgmtIP)
		claims := protocol.IdentityClaims{Locators: map[string]string{}}
		if f.ChassisID != "" {
			claims.ChassisIDs = []string{f.ChassisID}
		}
		if f.SysName != "" {
			claims.Hostnames = []string{f.SysName}
		}
		if f.MgmtIP != "" {
			claims.Locators["mgmt_ip"] = f.MgmtIP
		}
		devs = append(devs, protocol.DeviceObservation{
			ObsKey: obsKey, Site: site, Name: f.SysName, RoleHint: f.RoleHint,
			ChassisID: f.ChassisID, Description: f.SysDescr, PrimaryIP: f.MgmtIP,
			Status: "active", Claims: claims,
		})
		links = append(links, protocol.LinkObservation{
			ObsKey:           "raw-lldp:" + k,
			Site:             site,
			Method:           "lldp",
			LocalDeviceName:  localHost,
			LocalPort:        cf.Iface,
			RemoteObsKey:     obsKey,
			RemoteDeviceName: f.SysName,
			RemoteChassisID:  f.ChassisID,
			RemotePort:       f.PortID,
			RemotePortDescr:  f.PortDescr,
			RemoteIP:         f.MgmtIP,
			Description:      "Passive LLDP frame on " + cf.Iface,
		})
	}
	return devs, links
}

func parseLLDPCtlJSON(raw []byte, site, localHost string) ([]protocol.DeviceObservation, []protocol.LinkObservation) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, nil
	}
	lldpObj, _ := root["lldp"].(map[string]any)
	ifacesSlice, _ := lldpObj["interface"].([]any)
	var devs []protocol.DeviceObservation
	var links []protocol.LinkObservation

	for _, entry := range ifacesSlice {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		for ifName, val := range m {
			detail, ok := val.(map[string]any)
			if !ok {
				continue
			}
			chassisMap, _ := detail["chassis"].(map[string]any)
			portMap, _ := detail["port"].(map[string]any)

			var sysName, chassisID, descr, mgmtIP string
			for chKey, chVal := range chassisMap {
				sysName = chKey
				if chObj, ok := chVal.(map[string]any); ok {
					if idObj, ok := chObj["id"].(map[string]any); ok {
						chassisID, _ = idObj["value"].(string)
					}
					descr, _ = chObj["descr"].(string)
					if mgmt, ok := chObj["mgmt-ip"].(string); ok {
						mgmtIP = mgmt
					}
				}
			}
			var remPort, remDescr string
			if idObj, ok := portMap["id"].(map[string]any); ok {
				remPort, _ = idObj["value"].(string)
			}
			remDescr, _ = portMap["descr"].(string)

			obsKey := firstStr(chassisID, sysName, mgmtIP)
			if obsKey == "" {
				continue
			}
			claims := protocol.IdentityClaims{Locators: map[string]string{}}
			if chassisID != "" {
				claims.ChassisIDs = []string{chassisID}
				if norm, err := identity.NormalizeMAC(chassisID); err == nil {
					claims.MACs = []string{norm}
				}
			}
			if sysName != "" {
				claims.Hostnames = []string{sysName}
			}
			if mgmtIP != "" {
				claims.Locators["mgmt_ip"] = mgmtIP
			}
			devs = append(devs, protocol.DeviceObservation{
				ObsKey: obsKey, Site: site, Name: sysName, RoleHint: "switch",
				ChassisID: chassisID, Description: descr, PrimaryIP: mgmtIP,
				Status: "active", Claims: claims,
			})
			links = append(links, protocol.LinkObservation{
				ObsKey:           fmt.Sprintf("lldpctl:%s:%s", ifName, obsKey),
				Site:             site,
				Method:           "lldp",
				LocalDeviceName:  localHost,
				LocalPort:        ifName,
				RemoteObsKey:     obsKey,
				RemoteDeviceName: sysName,
				RemoteChassisID:  chassisID,
				RemotePort:       remPort,
				RemotePortDescr:  remDescr,
				RemoteIP:         mgmtIP,
				Description:      "lldpctl neighbor on " + ifName,
			})
		}
	}
	return devs, links
}

func htons(v uint16) uint16 {
	return (v<<8)&0xff00 | v>>8
}

func firstStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
