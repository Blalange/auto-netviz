package protocol

import "time"

// ObservationBatch carries one collector source's latest point-in-time observations.
type ObservationBatch struct {
	BatchID      string              `json:"batch_id"`
	CollectorID  string              `json:"collector_id"`
	Site         string              `json:"site"`
	Zone         string              `json:"zone,omitempty"`
	Source       string              `json:"source"`
	Profile      string              `json:"profile"`
	CollectedAt  time.Time           `json:"collected_at"`
	TTLSeconds   int                 `json:"ttl_seconds,omitempty"`
	FullSnapshot bool                `json:"full_snapshot"`
	Devices      []DeviceObservation `json:"devices,omitempty"`
	Links        []LinkObservation   `json:"links,omitempty"`
	VLANs        []VLANObservation   `json:"vlans,omitempty"`
	Prefixes     []PrefixObservation `json:"prefixes,omitempty"`
	Errors       []string            `json:"errors,omitempty"`
}

type IdentityClaims struct {
	UUIDs      []string          `json:"uuids,omitempty"`
	Serials    []string          `json:"serials,omitempty"`
	ChassisIDs []string          `json:"chassis_ids,omitempty"`
	MACs       []string          `json:"macs,omitempty"`
	Locators   map[string]string `json:"locators,omitempty"` // e.g., snmp_ip, redfish_ip, ipmi_ip, ssh_host, unifi_ip, k8s_node, cluster
	Hostnames  []string          `json:"hostnames,omitempty"`
}

type DeviceObservation struct {
	ObsKey          string                 `json:"obs_key"`
	Site            string                 `json:"site,omitempty"`
	Cluster         string                 `json:"cluster,omitempty"`
	Name            string                 `json:"name,omitempty"`
	RoleHint        string                 `json:"role_hint,omitempty"`
	Manufacturer    string                 `json:"manufacturer,omitempty"`
	Model           string                 `json:"model,omitempty"`
	ModelCode       string                 `json:"model_code,omitempty"`
	Platform        string                 `json:"platform,omitempty"`
	Status          string                 `json:"status,omitempty"`
	Serial          string                 `json:"serial,omitempty"`
	ServiceTag      string                 `json:"service_tag,omitempty"`
	SystemUUID      string                 `json:"system_uuid,omitempty"`
	ChassisID       string                 `json:"chassis_id,omitempty"`
	Description     string                 `json:"description,omitempty"`
	PowerState      string                 `json:"power_state,omitempty"`
	FirmwareVersion string                 `json:"firmware_version,omitempty"`
	OSVersion       string                 `json:"os_version,omitempty"`
	KernelVersion   string                 `json:"kernel_version,omitempty"`
	PrimaryIP       string                 `json:"primary_ip,omitempty"`
	OOBIP           string                 `json:"oob_ip,omitempty"`
	MgmtIP          string                 `json:"mgmt_ip,omitempty"`
	IsClient        bool                   `json:"is_client,omitempty"`
	IsWireless      bool                   `json:"is_wireless,omitempty"`
	Claims          IdentityClaims         `json:"claims"`
	Interfaces      []InterfaceObservation `json:"interfaces,omitempty"`
	Modules         []ModuleObservation    `json:"modules,omitempty"`
	Sensors         []SensorObservation    `json:"sensors,omitempty"`
	Attributes      map[string]any         `json:"attributes,omitempty"`
}

type InterfaceObservation struct {
	Name          string          `json:"name"`
	Candidates    []string        `json:"candidates,omitempty"`
	IfIndex       int             `json:"if_index,omitempty"`
	Type          string          `json:"type,omitempty"`
	Kind          string          `json:"kind,omitempty"` // physical | virtual | lag | wireless | oob
	Enabled       *bool           `json:"enabled,omitempty"`
	LinkUp        *bool           `json:"link_up,omitempty"`
	MgmtOnly      bool            `json:"mgmt_only,omitempty"`
	SpeedMbps     int             `json:"speed_mbps,omitempty"`
	MTU           int             `json:"mtu,omitempty"`
	MAC           string          `json:"mac,omitempty"`
	Description   string          `json:"description,omitempty"`
	Mode          string          `json:"mode,omitempty"` // access | tagged | tagged-all
	UntaggedVLAN  int             `json:"untagged_vlan,omitempty"`
	TaggedVLANs   []int           `json:"tagged_vlans,omitempty"`
	PoEMode       string          `json:"poe_mode,omitempty"`
	PoEWatts      float64         `json:"poe_watts,omitempty"`
	IPAddresses   []IPObservation `json:"ip_addresses,omitempty"`
	LagMembers    []string        `json:"lag_members,omitempty"`
	IsTrunk       bool            `json:"is_trunk,omitempty"`
	BridgePort    int             `json:"bridge_port,omitempty"`
	LearnedMACs   []string        `json:"learned_macs,omitempty"`
}

type IPObservation struct {
	Address     string `json:"address"` // CIDR or plain IP
	DNSName     string `json:"dns_name,omitempty"`
	Description string `json:"description,omitempty"`
	Role        string `json:"role,omitempty"`
	Primary     bool   `json:"primary,omitempty"`
}

type ModuleObservation struct {
	Bay          string         `json:"bay"`
	BayType      string         `json:"bay_type"`
	BayDesc      string         `json:"bay_description,omitempty"`
	Profile      string         `json:"profile"` // CPU | Memory | Hard disk | Fan | Power supply | Expansion card | GPU | Transceiver
	Manufacturer string         `json:"manufacturer,omitempty"`
	Model        string         `json:"model"`
	PartNumber   string         `json:"part_number,omitempty"`
	Serial       string         `json:"serial,omitempty"`
	Status       string         `json:"status,omitempty"`
	Description  string         `json:"description,omitempty"`
	Attributes   map[string]any `json:"attributes,omitempty"`
}

type SensorObservation struct {
	Name   string  `json:"name"`
	Kind   string  `json:"kind"` // fan | psu | temp | power | voltage
	Value  float64 `json:"value,omitempty"`
	Unit   string  `json:"unit,omitempty"`
	Status string  `json:"status,omitempty"`
}

// LinkObservation represents a discovered L1/L2 neighbor or client attachment.
type LinkObservation struct {
	ObsKey            string `json:"obs_key"`
	Site              string `json:"site,omitempty"`
	Method            string `json:"method"` // lldp | cdp | unifi_uplink | unifi_client | snmp_fdb | arp
	LocalObsKey       string `json:"local_obs_key,omitempty"`
	LocalDeviceName   string `json:"local_device_name,omitempty"`
	LocalMAC          string `json:"local_mac,omitempty"`
	LocalIP           string `json:"local_ip,omitempty"`
	LocalPort         string `json:"local_port,omitempty"`
	LocalPortCandidates []string `json:"local_port_candidates,omitempty"`
	RemoteObsKey      string `json:"remote_obs_key,omitempty"`
	RemoteDeviceName  string `json:"remote_device_name,omitempty"`
	RemoteChassisID   string `json:"remote_chassis_id,omitempty"`
	RemoteMAC         string `json:"remote_mac,omitempty"`
	RemoteIP          string `json:"remote_ip,omitempty"`
	RemotePort        string `json:"remote_port,omitempty"`
	RemotePortDescr   string `json:"remote_port_descr,omitempty"`
	RemotePortCandidates []string `json:"remote_port_candidates,omitempty"`
	CableType         string `json:"cable_type,omitempty"`
	SpeedMbps         int    `json:"speed_mbps,omitempty"`
	VLAN              int    `json:"vlan,omitempty"`
	Verified          bool   `json:"verified,omitempty"`
	Description       string `json:"description,omitempty"`
}

type VLANObservation struct {
	Site        string `json:"site"`
	VID         int    `json:"vid"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type PrefixObservation struct {
	CIDR        string `json:"cidr"`
	Site        string `json:"site"`
	VLAN        int    `json:"vlan,omitempty"`
	Gateway     string `json:"gateway,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	Description string `json:"description,omitempty"`
}
