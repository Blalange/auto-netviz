package config

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blalange/auto-netviz/internal/protocol"
	"gopkg.in/yaml.v3"
)

type Config struct {
	CollectorID       string            `yaml:"collector_id"`
	Name              string            `yaml:"name"`
	Site              string            `yaml:"site"`
	Zone              string            `yaml:"zone"`
	ServerWSURL       string            `yaml:"server_ws_url"`
	SpoolDir          string            `yaml:"spool_dir"`
	HeartbeatInterval string            `yaml:"heartbeat_interval"`
	Schedules         map[string]string `yaml:"schedules"`
	Defaults          DefaultsConfig    `yaml:"defaults"`
	Sources           SourcesConfig     `yaml:"sources"`
}

type DefaultsConfig struct {
	UnifiModels    map[string]string `yaml:"unifi_models"`
	UnifiVLANNames map[int]string    `yaml:"unifi_vlan_names"`
}

type SourcesConfig struct {
	Unifi      []UnifiProfile      `yaml:"unifi"`
	SNMP       []SNMPProfile       `yaml:"snmp"`
	LLDP       []LLDPProfile       `yaml:"lldp"`
	Redfish    []RedfishProfile    `yaml:"redfish"`
	IPMI       []IPMIProfile       `yaml:"ipmi"`
	Discovery  []DiscoveryProfile  `yaml:"discovery"`
	HostOS     []HostOSProfile     `yaml:"hostos"`
	Kubernetes []KubernetesProfile `yaml:"kubernetes"`
}

type MatchRule struct {
	Sites   []string `yaml:"sites"`
	Devices []string `yaml:"devices"`
	Tags    []string `yaml:"tags"`
	Roles   []string `yaml:"roles"`
	CIDRs   []string `yaml:"cidrs"`
}

type UnifiProfile struct {
	Name           string            `yaml:"name"`
	Enabled        *bool             `yaml:"enabled"`
	Site           string            `yaml:"site"`
	URL            SecretValue       `yaml:"url"`
	UniFiSite      string            `yaml:"unifi_site"`
	APIKey         SecretValue       `yaml:"api_key"`
	Username       SecretValue       `yaml:"username"`
	Password       SecretValue       `yaml:"password"`
	VerifyTLS      bool              `yaml:"verify_tls"`
	Timeout        string            `yaml:"timeout"`
	SNMPCredential string            `yaml:"snmp_credential"`
	IncludeClients *bool             `yaml:"include_clients"`
	Models         map[string]string `yaml:"models"`
	VLANNames      map[int]string    `yaml:"vlan_names"`
}

type StaticTarget struct {
	Name       string `yaml:"name"`
	IP         string `yaml:"ip"`
	Host       string `yaml:"host"`
	Site       string `yaml:"site"`
	Role       string `yaml:"role"`
	DeviceType string `yaml:"device_type"`
	Interfaces bool   `yaml:"interfaces"`
	Cables     bool   `yaml:"cables"`
}

type SNMPProfile struct {
	Name         string         `yaml:"name"`
	Enabled      *bool          `yaml:"enabled"`
	Version      string         `yaml:"version"` // 2c | 3
	Community    SecretValue    `yaml:"community"`
	Username     SecretValue    `yaml:"username"`
	AuthProtocol string         `yaml:"auth_protocol"` // MD5 | SHA | SHA256
	AuthPass     SecretValue    `yaml:"auth_password"`
	PrivProtocol string         `yaml:"priv_protocol"` // DES | AES
	PrivPass     SecretValue    `yaml:"priv_password"`
	Timeout      string         `yaml:"timeout"`
	Retries      int            `yaml:"retries"`
	AutoUniFi    *bool          `yaml:"auto_unifi"`
	PortNames    string         `yaml:"port_names"` // auto | port-number | ifname
	Targets      []StaticTarget `yaml:"targets"`
	Match        *MatchRule     `yaml:"match"`
	Default      bool           `yaml:"default"`
}

type LLDPProfile struct {
	Name       string   `yaml:"name"`
	Enabled    *bool    `yaml:"enabled"`
	Site       string   `yaml:"site"`
	PassiveRaw bool     `yaml:"passive_raw"`
	Interfaces []string `yaml:"interfaces"`
	LLDPCtl    *bool    `yaml:"lldpctl"`
}

type RedfishProfile struct {
	Name        string         `yaml:"name"`
	Enabled     *bool          `yaml:"enabled"`
	Username    SecretValue    `yaml:"username"`
	Password    SecretValue    `yaml:"password"`
	VerifyTLS   bool           `yaml:"verify_tls"`
	Timeout     string         `yaml:"timeout"`
	SystemPath  string         `yaml:"system_path"`
	ChassisPath string         `yaml:"chassis_path"`
	Targets     []StaticTarget `yaml:"targets"`
	Match       *MatchRule     `yaml:"match"`
	Default     bool           `yaml:"default"`
}

type IPMIProfile struct {
	Name      string         `yaml:"name"`
	Enabled   *bool          `yaml:"enabled"`
	Username  SecretValue    `yaml:"username"`
	Password  SecretValue    `yaml:"password"`
	Binary    string         `yaml:"binary"`
	Interface string         `yaml:"interface"`
	Privilege string         `yaml:"privilege"`
	Cipher    string         `yaml:"cipher"`
	Timeout   string         `yaml:"timeout"`
	Targets   []StaticTarget `yaml:"targets"`
	Match     *MatchRule     `yaml:"match"`
	Default   bool           `yaml:"default"`
}

type DiscoveryProfile struct {
	Name           string   `yaml:"name"`
	Enabled        *bool    `yaml:"enabled"`
	Site           string   `yaml:"site"`
	CIDRs          []string `yaml:"cidrs"`
	Exclude        []string `yaml:"exclude"`
	ReadLocalARP   *bool    `yaml:"read_local_arp"`
	ProbePorts     []int    `yaml:"probe_ports"`
	ProbeRedfish   *bool    `yaml:"probe_redfish"`
	ProbeSNMP      *bool    `yaml:"probe_snmp"`
	ReverseDNS     *bool    `yaml:"reverse_dns"`
	DHCPLeaseFiles []string `yaml:"dhcp_lease_files"`
	Concurrency    int      `yaml:"concurrency"`
	Timeout        string   `yaml:"timeout"`
}

type SSHTarget struct {
	Name                  string      `yaml:"name"`
	Host                  string      `yaml:"host"`
	Port                  int         `yaml:"port"`
	Site                  string      `yaml:"site"`
	User                  string      `yaml:"user"`
	Password              SecretValue `yaml:"password"`
	PrivateKeyPath        string      `yaml:"private_key_path"`
	InsecureIgnoreHostKey bool        `yaml:"insecure_ignore_host_key"`
}

type HostOSProfile struct {
	Name     string      `yaml:"name"`
	Enabled  *bool       `yaml:"enabled"`
	Site     string      `yaml:"site"`
	Local    bool        `yaml:"local"`
	User     string      `yaml:"user"`
	Password SecretValue `yaml:"password"`
	KeyPath  string      `yaml:"private_key_path"`
	Targets  []SSHTarget `yaml:"targets"`
	Match    *MatchRule  `yaml:"match"`
	Default  bool        `yaml:"default"`
}

type KubernetesProfile struct {
	Name           string      `yaml:"name"`
	Enabled        *bool       `yaml:"enabled"`
	Site           string      `yaml:"site"`
	Cluster        string      `yaml:"cluster"`
	InCluster      bool        `yaml:"in_cluster"`
	Kubeconfig     SecretValue `yaml:"kubeconfig"`
	TalosConfig    SecretValue `yaml:"talosconfig"`
	TalosCtl       string      `yaml:"talosctl"`
	TalosConfigDir string      `yaml:"talos_config_dir"`
}

func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		CollectorID:       defaultHostname(),
		Name:              defaultHostname(),
		ServerWSURL:       "ws://127.0.0.1:8080/ws/collector",
		SpoolDir:          "./spool",
		HeartbeatInterval: "15s",
		Schedules: map[string]string{
			"unifi":      "60s",
			"snmp":       "60s",
			"lldp":       "30s",
			"redfish":    "120s",
			"ipmi":       "120s",
			"discovery":  "180s",
			"hostos":     "120s",
			"kubernetes": "45s",
		},
	}
	if path == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read collector config %q: %w", path, err)
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse collector config %q: %w", path, err)
	}
	if cfg.CollectorID == "" {
		cfg.CollectorID = defaultHostname()
	}
	if cfg.Name == "" {
		cfg.Name = cfg.CollectorID
	}
	return cfg, nil
}

func defaultHostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "auto-netviz-collector"
	}
	return h
}

func ParseDuration(raw string, def time.Duration) time.Duration {
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// MatchTarget evaluates profile selection rules: explicit name -> match rule -> default -> single enabled profile.
func (m *MatchRule) Matches(site, deviceName, role string, tags []string, addr string) bool {
	if m == nil {
		return false
	}
	if len(m.Sites) > 0 && !containsIgnoreCase(m.Sites, site) {
		return false
	}
	if len(m.Roles) > 0 && !containsIgnoreCase(m.Roles, role) {
		return false
	}
	if len(m.Tags) > 0 {
		found := false
		for _, t := range tags {
			if containsIgnoreCase(m.Tags, t) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(m.Devices) > 0 {
		matched := false
		for _, pat := range m.Devices {
			if ok, _ := filepath.Match(strings.ToLower(pat), strings.ToLower(deviceName)); ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(m.CIDRs) > 0 {
		parsedAddr, err := netip.ParseAddr(addr)
		if err != nil {
			return false
		}
		matched := false
		for _, c := range m.CIDRs {
			if pfx, err := netip.ParsePrefix(c); err == nil && pfx.Contains(parsedAddr) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func containsIgnoreCase(list []string, target string) bool {
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func IsEnabled(b *bool) bool {
	if b == nil {
		return true
	}
	return *b
}

func MatchSNMPProfile(profiles []SNMPProfile, explicit, site, deviceName, role string, tags []string, addr string) (*SNMPProfile, error) {
	var enabled []SNMPProfile
	for _, p := range profiles {
		if IsEnabled(p.Enabled) {
			enabled = append(enabled, p)
		}
	}
	if len(enabled) == 0 {
		return nil, fmt.Errorf("no enabled SNMP profiles")
	}
	if explicit != "" {
		for i := range enabled {
			if enabled[i].Name == explicit {
				return &enabled[i], nil
			}
		}
		return nil, fmt.Errorf("SNMP profile %q not found", explicit)
	}
	for i := range enabled {
		if enabled[i].Match != nil && enabled[i].Match.Matches(site, deviceName, role, tags, addr) {
			return &enabled[i], nil
		}
	}
	for i := range enabled {
		if enabled[i].Default {
			return &enabled[i], nil
		}
	}
	if len(enabled) == 1 {
		return &enabled[0], nil
	}
	return nil, fmt.Errorf("multiple SNMP profiles configured and none matched target %s (%s)", deviceName, addr)
}

func MatchRedfishProfile(profiles []RedfishProfile, explicit, site, deviceName, role string, tags []string, addr string) (*RedfishProfile, error) {
	var enabled []RedfishProfile
	for _, p := range profiles {
		if IsEnabled(p.Enabled) {
			enabled = append(enabled, p)
		}
	}
	if len(enabled) == 0 {
		return nil, fmt.Errorf("no enabled Redfish profiles")
	}
	if explicit != "" {
		for i := range enabled {
			if enabled[i].Name == explicit {
				return &enabled[i], nil
			}
		}
		return nil, fmt.Errorf("Redfish profile %q not found", explicit)
	}
	for i := range enabled {
		if enabled[i].Match != nil && enabled[i].Match.Matches(site, deviceName, role, tags, addr) {
			return &enabled[i], nil
		}
	}
	for i := range enabled {
		if enabled[i].Default {
			return &enabled[i], nil
		}
	}
	if len(enabled) == 1 {
		return &enabled[0], nil
	}
	return nil, fmt.Errorf("multiple Redfish profiles configured and none matched target %s (%s)", deviceName, addr)
}

func MatchIPMIProfile(profiles []IPMIProfile, explicit, site, deviceName, role string, tags []string, addr string) (*IPMIProfile, error) {
	var enabled []IPMIProfile
	for _, p := range profiles {
		if IsEnabled(p.Enabled) {
			enabled = append(enabled, p)
		}
	}
	if len(enabled) == 0 {
		return nil, fmt.Errorf("no enabled IPMI profiles")
	}
	if explicit != "" {
		for i := range enabled {
			if enabled[i].Name == explicit {
				return &enabled[i], nil
			}
		}
		return nil, fmt.Errorf("IPMI profile %q not found", explicit)
	}
	for i := range enabled {
		if enabled[i].Match != nil && enabled[i].Match.Matches(site, deviceName, role, tags, addr) {
			return &enabled[i], nil
		}
	}
	for i := range enabled {
		if enabled[i].Default {
			return &enabled[i], nil
		}
	}
	if len(enabled) == 1 {
		return &enabled[0], nil
	}
	return nil, fmt.Errorf("multiple IPMI profiles configured and none matched target %s (%s)", deviceName, addr)
}

// Unused helper check placeholder for context
var _ = context.Background
var _ = protocol.TargetHint{}
