package manual

import "github.com/blalange/auto-netviz/internal/model"

// Document represents one YAML file (or YAML sub-document) in the manual inventory directory.
type Document struct {
	SchemaVersion int                 `yaml:"schema_version,omitempty"`
	Defaults      *Defaults           `yaml:"defaults,omitempty"`
	Sites         []SiteSpec          `yaml:"sites,omitempty"`
	Locations     []LocationSpec      `yaml:"locations,omitempty"`
	OwnerGroups   []OwnerGroupSpec    `yaml:"owner_groups,omitempty"`
	Owners        []OwnerSpec         `yaml:"owners,omitempty"`
	Contacts      []ContactSpec       `yaml:"contacts,omitempty"`
	DeviceTypes   []DeviceTypeSpec    `yaml:"device_types,omitempty"`
	ModuleTypes   []ModuleTypeSpec    `yaml:"module_types,omitempty"`
	Racks         []RackSpec          `yaml:"racks,omitempty"`
	VLANs         []VLANSpec          `yaml:"vlans,omitempty"`
	Prefixes      []PrefixSpec        `yaml:"prefixes,omitempty"`
	Devices       []DeviceSpec        `yaml:"devices,omitempty"`
	Cables        []CableSpec         `yaml:"cables,omitempty"`
	Assets        []AssetSpec         `yaml:"assets,omitempty"`
	Rules         *RulesSpec          `yaml:"rules,omitempty"`
}

type Defaults struct {
	Site        string   `yaml:"site,omitempty"`
	Location    string   `yaml:"location,omitempty"`
	Owner       string   `yaml:"owner,omitempty"`
	Rack        string   `yaml:"rack,omitempty"`
	Status      string   `yaml:"status,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	ComputeRole string   `yaml:"compute_role,omitempty"`
}

type SiteSpec struct {
	ID          string `yaml:"id,omitempty"`
	Name        string `yaml:"name"`
	Status      string `yaml:"status,omitempty"`
	Facility    string `yaml:"facility,omitempty"`
	Address     string `yaml:"address,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type LocationSpec struct {
	ID          string `yaml:"id,omitempty"`
	Site        string `yaml:"site,omitempty"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type OwnerGroupSpec struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type OwnerSpec struct {
	Name        string `yaml:"name"`
	Group       string `yaml:"group,omitempty"`
	Email       string `yaml:"email,omitempty"`
	Link        string `yaml:"link,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type ContactSpec struct {
	Name  string `yaml:"name"`
	Role  string `yaml:"role,omitempty"`
	Owner string `yaml:"owner,omitempty"`
	Email string `yaml:"email,omitempty"`
	Phone string `yaml:"phone,omitempty"`
	Notes string `yaml:"notes,omitempty"`
}

type FaceplateElementSpec struct {
	Kind       string   `yaml:"kind"`
	Label      string   `yaml:"label,omitempty"`
	Connector  string   `yaml:"connector,omitempty"`
	FormFactor string   `yaml:"form_factor,omitempty"`
	Rows       int      `yaml:"rows,omitempty"`
	Cols       int      `yaml:"cols,omitempty"`
	Start      int      `yaml:"start,omitempty"`
	Count      int      `yaml:"count,omitempty"`
	Width      int      `yaml:"width,omitempty"`
	Prefix     string   `yaml:"prefix,omitempty"`
	Names      []string `yaml:"names,omitempty"`
	PoE        bool     `yaml:"poe,omitempty"`
}

type FaceplateSpec struct {
	Front []FaceplateElementSpec `yaml:"front,omitempty"`
	Rear  []FaceplateElementSpec `yaml:"rear,omitempty"`
}

type DeviceTypeSpec struct {
	ID           string         `yaml:"id,omitempty"`
	Manufacturer string         `yaml:"manufacturer"`
	Model        string         `yaml:"model"`
	UHeight      int            `yaml:"u_height,omitempty"`
	Depth        string         `yaml:"depth,omitempty"`
	RoleHint     string         `yaml:"role_hint,omitempty"`
	PartNumber   string         `yaml:"part_number,omitempty"`
	Airflow      string         `yaml:"airflow,omitempty"`
	WeightKg     float64        `yaml:"weight_kg,omitempty"`
	Description  string         `yaml:"description,omitempty"`
	Faceplate    *FaceplateSpec `yaml:"faceplate,omitempty"`
}

type ModuleTypeSpec struct {
	ID           string         `yaml:"id,omitempty"`
	Manufacturer string         `yaml:"manufacturer"`
	Model        string         `yaml:"model"`
	Profile      string         `yaml:"profile"`
	BayType      string         `yaml:"bay_type,omitempty"`
	PartNumber   string         `yaml:"part_number,omitempty"`
	Description  string         `yaml:"description,omitempty"`
	Attributes   map[string]any `yaml:"attributes,omitempty"`
}

type RackLayoutEntry struct {
	Gap    int    `yaml:"gap,omitempty"`
	Empty  int    `yaml:"empty,omitempty"`
	Device string `yaml:"device,omitempty"`
	Face   string `yaml:"face,omitempty"`
}

type RackSpec struct {
	Name         string            `yaml:"name"`
	Site         string            `yaml:"site,omitempty"`
	Location     string            `yaml:"location,omitempty"`
	Status       string            `yaml:"status,omitempty"`
	Width        int               `yaml:"width,omitempty"`
	UHeight      int               `yaml:"u_height,omitempty"`
	DescUnits    bool              `yaml:"desc_units,omitempty"`
	PowerBudgetW int               `yaml:"power_budget_w,omitempty"`
	Description  string            `yaml:"description,omitempty"`
	Layout       []RackLayoutEntry `yaml:"layout,omitempty"`
}

type VLANSpec struct {
	Site        string `yaml:"site,omitempty"`
	VID         int    `yaml:"vid"`
	Name        string `yaml:"name"`
	Status      string `yaml:"status,omitempty"`
	Color       string `yaml:"color,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type PrefixSpec struct {
	CIDR        string `yaml:"cidr"`
	Site        string `yaml:"site,omitempty"`
	VLAN        *int   `yaml:"vlan,omitempty"`
	Gateway     string `yaml:"gateway,omitempty"`
	Status      string `yaml:"status,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type IdentitySpec struct {
	ServiceTag string   `yaml:"service_tag,omitempty"`
	Serial     string   `yaml:"serial,omitempty"`
	SystemUUID string   `yaml:"system_uuid,omitempty"`
	ChassisID  string   `yaml:"chassis_id,omitempty"`
	MACs       []string `yaml:"macs,omitempty"`
}

type EndpointSpec struct {
	IP         string `yaml:"ip,omitempty"`
	Host       string `yaml:"host,omitempty"`
	Profile    string `yaml:"profile,omitempty"`
	Interfaces bool   `yaml:"interfaces,omitempty"`
	Cables     bool   `yaml:"cables,omitempty"`
}

type UniFiLocatorSpec struct {
	IP     string `yaml:"ip,omitempty"`
	MAC    string `yaml:"mac,omitempty"`
	Serial string `yaml:"serial,omitempty"`
}

type KubernetesLocatorSpec struct {
	Node    string `yaml:"node,omitempty"`
	Cluster string `yaml:"cluster,omitempty"`
}

type CollectSpec struct {
	Collector  string                 `yaml:"collector,omitempty"`
	SNMP       *EndpointSpec          `yaml:"snmp,omitempty"`
	Redfish    *EndpointSpec          `yaml:"redfish,omitempty"`
	IPMI       *EndpointSpec          `yaml:"ipmi,omitempty"`
	SSH        *EndpointSpec          `yaml:"ssh,omitempty"`
	UniFi      *UniFiLocatorSpec      `yaml:"unifi,omitempty"`
	Kubernetes *KubernetesLocatorSpec `yaml:"kubernetes,omitempty"`
}

type IPSpec struct {
	Address     string `yaml:"address"`
	DNSName     string `yaml:"dns_name,omitempty"`
	Description string `yaml:"description,omitempty"`
	Role        string `yaml:"role,omitempty"`
	Status      string `yaml:"status,omitempty"`
	Primary     bool   `yaml:"primary,omitempty"`
}

type InterfaceSpec struct {
	Name         string   `yaml:"name"`
	Type         string   `yaml:"type,omitempty"`
	Kind         string   `yaml:"kind,omitempty"`
	Enabled      *bool    `yaml:"enabled,omitempty"`
	MgmtOnly     bool     `yaml:"mgmt_only,omitempty"`
	SpeedMbps    int      `yaml:"speed_mbps,omitempty"`
	MTU          int      `yaml:"mtu,omitempty"`
	MACAddress   string   `yaml:"mac_address,omitempty"`
	Description  string   `yaml:"description,omitempty"`
	Mode         string   `yaml:"mode,omitempty"`
	UntaggedVLAN *int     `yaml:"untagged_vlan,omitempty"`
	TaggedVLANs  []int    `yaml:"tagged_vlans,omitempty"`
	IPAddresses  []IPSpec `yaml:"ip_addresses,omitempty"`
}

type ModuleBaySpec struct {
	Name        string `yaml:"name"`
	Position    string `yaml:"position,omitempty"`
	BayType     string `yaml:"bay_type,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type DeviceModuleSpec struct {
	Bay         string `yaml:"bay"`
	ModuleType  string `yaml:"module_type"`
	Status      string `yaml:"status,omitempty"`
	Serial      string `yaml:"serial,omitempty"`
	AssetTag    string `yaml:"asset_tag,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type UplinkSpec struct {
	Interface string `yaml:"interface"`
	Switch    string `yaml:"switch"`
	Port      string `yaml:"port"`
	Type      string `yaml:"type,omitempty"`
	Color     string `yaml:"color,omitempty"`
}

type DeviceContactSpec struct {
	Contact string `yaml:"contact"`
	Role    string `yaml:"role,omitempty"`
}

type DeviceSpec struct {
	Name          string              `yaml:"name"`
	Site          string              `yaml:"site,omitempty"`
	Location      string              `yaml:"location,omitempty"`
	Role          string              `yaml:"role,omitempty"`
	DeviceType    string              `yaml:"device_type,omitempty"`
	Platform      string              `yaml:"platform,omitempty"`
	Status        string              `yaml:"status,omitempty"`
	Rack          string              `yaml:"rack,omitempty"`
	Position      *int                `yaml:"position,omitempty"`
	Face          string              `yaml:"face,omitempty"`
	Serial        string              `yaml:"serial,omitempty"`
	AssetTag      string              `yaml:"asset_tag,omitempty"`
	WarrantyUntil string              `yaml:"warranty_until,omitempty"`
	Owner         string              `yaml:"owner,omitempty"`
	OOBIP         string              `yaml:"oob_ip,omitempty"`
	Description   string              `yaml:"description,omitempty"`
	Comments      string              `yaml:"comments,omitempty"`
	Tags          []string            `yaml:"tags,omitempty"`
	Contacts      []DeviceContactSpec `yaml:"contacts,omitempty"`
	Identity      *IdentitySpec       `yaml:"identity,omitempty"`
	Collect       *CollectSpec        `yaml:"collect,omitempty"`
	Interfaces    []InterfaceSpec     `yaml:"interfaces,omitempty"`
	ModuleBays    []ModuleBaySpec     `yaml:"module_bays,omitempty"`
	Modules       []DeviceModuleSpec  `yaml:"modules,omitempty"`
	Uplinks       []UplinkSpec        `yaml:"uplinks,omitempty"`
	Custom        map[string]any      `yaml:"custom,omitempty"`
}

type CableEndpointSpec struct {
	Device string `yaml:"device"`
	Port   string `yaml:"port"`
}

type CableSpec struct {
	A           CableEndpointSpec `yaml:"a"`
	B           CableEndpointSpec `yaml:"b"`
	Type        string            `yaml:"type,omitempty"`
	Status      string            `yaml:"status,omitempty"`
	Color       string            `yaml:"color,omitempty"`
	Label       string            `yaml:"label,omitempty"`
	Description string            `yaml:"description,omitempty"`
}

type AssetSpec struct {
	AssetTag      string         `yaml:"asset_tag,omitempty"`
	Name          string         `yaml:"name"`
	Category      string         `yaml:"category,omitempty"`
	Manufacturer  string         `yaml:"manufacturer,omitempty"`
	Model         string         `yaml:"model,omitempty"`
	Serial        string         `yaml:"serial,omitempty"`
	Status        string         `yaml:"status,omitempty"`
	Site          string         `yaml:"site,omitempty"`
	Location      string         `yaml:"location,omitempty"`
	Owner         string         `yaml:"owner,omitempty"`
	PurchaseDate  string         `yaml:"purchase_date,omitempty"`
	WarrantyUntil string         `yaml:"warranty_until,omitempty"`
	Notes         string         `yaml:"notes,omitempty"`
	Custom        map[string]any `yaml:"custom,omitempty"`
}

type AutoCreateRules struct {
	NetworkDevices  *bool `yaml:"network_devices,omitempty"`
	Servers         *bool `yaml:"servers,omitempty"`
	WiredClients    *bool `yaml:"wired_clients,omitempty"`
	WirelessClients *bool `yaml:"wireless_clients,omitempty"`
}

type IgnoreRules struct {
	CIDRs       []string `yaml:"cidrs,omitempty"`
	MACPrefixes []string `yaml:"mac_prefixes,omitempty"`
	Hostnames   []string `yaml:"hostnames,omitempty"`
}

type RulesSpec struct {
	AutoCreate *AutoCreateRules `yaml:"auto_create,omitempty"`
	Ignore     *IgnoreRules     `yaml:"ignore,omitempty"`
}

// Inventory is the merged, validated, and expanded manual YAML model.
type Inventory struct {
	Dir         string
	Hash        string
	Defaults    Defaults
	Sites       []SiteSpec
	Locations   []LocationSpec
	OwnerGroups []OwnerGroupSpec
	Owners      []OwnerSpec
	Contacts    []ContactSpec
	DeviceTypes []DeviceTypeSpec
	ModuleTypes []ModuleTypeSpec
	Racks       []RackSpec
	VLANs       []VLANSpec
	Prefixes    []PrefixSpec
	Devices     []DeviceSpec
	Cables      []CableSpec
	Assets      []AssetSpec
	Rules       EffectiveRules
	RackPlans   map[string]RackPlan // key: site + "/" + rackName
	Issues      []model.Issue
	FilesLoaded []string
}

type EffectiveRules struct {
	AutoNetworkDevices  bool
	AutoServers         bool
	AutoWiredClients    bool
	AutoWirelessClients bool
	IgnoreCIDRs         []string
	IgnoreMACPrefixes   []string
	IgnoreHostnames     []string
}
