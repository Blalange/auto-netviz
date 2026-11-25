package manual

import (
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/protocol"
)

// BuildTargetHints extracts non-secret discovery/polling locators from manual devices
// so remote collectors can poll devices declared in YAML without duplicating IPs.
func (inv *Inventory) BuildTargetHints(collectorID, site string) []protocol.TargetHint {
	if inv == nil {
		return nil
	}
	hints := make([]protocol.TargetHint, 0, len(inv.Devices))
	for _, dev := range inv.Devices {
		if site != "" && dev.Site != "" && dev.Site != site {
			continue
		}
		if dev.Collect != nil && dev.Collect.Collector != "" && collectorID != "" && dev.Collect.Collector != collectorID {
			continue
		}

		h := protocol.TargetHint{
			DeviceName: dev.Name,
			Site:       dev.Site,
			Role:       dev.Role,
			DeviceType: dev.DeviceType,
			Tags:       append([]string(nil), dev.Tags...),
			OOBIP:      dev.OOBIP,
		}
		macSet := make(map[string]struct{})
		if dev.Identity != nil {
			for _, m := range dev.Identity.MACs {
				if norm, err := identity.NormalizeMAC(m); err == nil {
					macSet[norm] = struct{}{}
				}
			}
		}
		for _, iface := range dev.Interfaces {
			if iface.MACAddress != "" {
				if norm, err := identity.NormalizeMAC(iface.MACAddress); err == nil {
					macSet[norm] = struct{}{}
				}
			}
		}
		for m := range macSet {
			h.MACs = append(h.MACs, m)
		}

		if dev.Collect != nil {
			h.Collector = dev.Collect.Collector
			if dev.Collect.SNMP != nil {
				h.SNMPIP = dev.Collect.SNMP.IP
				h.SNMPCredential = dev.Collect.SNMP.Profile
				h.SNMPInterfaces = dev.Collect.SNMP.Interfaces
				h.SNMPCables = dev.Collect.SNMP.Cables
			}
			if dev.Collect.Redfish != nil {
				h.RedfishIP = dev.Collect.Redfish.IP
				h.RedfishCredential = dev.Collect.Redfish.Profile
			}
			if dev.Collect.IPMI != nil {
				h.IPMIIP = dev.Collect.IPMI.IP
				h.IPMICredential = dev.Collect.IPMI.Profile
			}
			if dev.Collect.SSH != nil {
				h.SSHHost = dev.Collect.SSH.Host
				if h.SSHHost == "" {
					h.SSHHost = dev.Collect.SSH.IP
				}
				h.SSHCredential = dev.Collect.SSH.Profile
			}
			if dev.Collect.UniFi != nil {
				h.UniFiIP = dev.Collect.UniFi.IP
				h.UniFiMAC = dev.Collect.UniFi.MAC
				h.UniFiSerial = dev.Collect.UniFi.Serial
			}
			if dev.Collect.Kubernetes != nil {
				h.TalosNode = dev.Collect.Kubernetes.Node
				h.TalosCluster = dev.Collect.Kubernetes.Cluster
			}
		}

		if h.SNMPIP != "" || h.RedfishIP != "" || h.IPMIIP != "" || h.SSHHost != "" ||
			h.UniFiIP != "" || h.UniFiMAC != "" || h.UniFiSerial != "" || h.TalosNode != "" || h.OOBIP != "" {
			hints = append(hints, h)
		}
	}
	return hints
}
