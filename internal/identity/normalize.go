package identity

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

var (
	reNonAlnum     = regexp.MustCompile(`[^a-z0-9]+`)
	rePortN        = regexp.MustCompile(`(?i)^port\s*(\d+)$`)
	reSlashPort    = regexp.MustCompile(`^(?:\d+/)+(\d+)$`)
	reEthPort      = regexp.MustCompile(`(?i)^eth(\d+)$`)
	reSwpPort      = regexp.MustCompile(`(?i)^swp(\d+)$`)
	reVendorPort   = regexp.MustCompile(`(?i)^(?:gi|gigabitethernet|te|tengigabitethernet|fa|fastethernet|fo|fortyGigabitEthernet|hu|eth|ethernet)(?:\d+/)+(\d+)$`)
	reDigits       = regexp.MustCompile(`(\d+)`)
	reHex12        = regexp.MustCompile(`^[0-9a-f]{12}$`)
	reUUID         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reUUIDCompact  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	junkSerialList = map[string]struct{}{
		"":                      {},
		"NONE":                  {},
		"N/A":                   {},
		"NA":                    {},
		"NULL":                  {},
		"UNKNOWN":               {},
		"DEFAULT":               {},
		"DEFAULT STRING":        {},
		"TO BE FILLED BY O.E.M.": {},
		"NOT SPECIFIED":         {},
		"NOT AVAILABLE":         {},
		"SYSTEM SERIAL NUMBER":  {},
		"CHASSIS SERIAL NUMBER": {},
		"BOARD SERIAL NUMBER":   {},
		"0":                     {},
		"00000000":              {},
		"0000000000":            {},
		"000000000000":          {},
		"123456789":             {},
		"1234567890":            {},
		"FFFFFFFF":              {},
		"XXXXXXXX":              {},
		"INVALID":               {},
	}
	junkUUIDList = map[string]struct{}{
		"":                                     {},
		"00000000-0000-0000-0000-000000000000": {},
		"ffffffff-ffff-ffff-ffff-ffffffffffff": {},
		"03000200-0400-0500-0006-000700080009": {},
		"not settable":                         {},
		"not present":                          {},
	}
)

// NormalizeMAC parses common MAC formats and returns lowercase colon-delimited aa:bb:cc:dd:ee:ff.
func NormalizeMAC(raw string) (string, error) {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return "", fmt.Errorf("empty MAC")
	}
	s = strings.TrimPrefix(s, "0x")
	cleaned := strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(s)
	if !reHex12.MatchString(cleaned) {
		return "", fmt.Errorf("invalid MAC %q", raw)
	}
	parts := make([]string, 6)
	for i := 0; i < 6; i++ {
		parts[i] = cleaned[i*2 : i*2+2]
	}
	return strings.Join(parts, ":"), nil
}

// IsInvalidMAC reports whether the MAC is unusable as a hardware identifier (zero, broadcast, multicast).
func IsInvalidMAC(mac string) bool {
	norm, err := NormalizeMAC(mac)
	if err != nil {
		return true
	}
	if norm == "00:00:00:00:00:00" || norm == "ff:ff:ff:ff:ff:ff" {
		return true
	}
	b, err := strconv.ParseUint(norm[0:2], 16, 8)
	if err != nil {
		return true
	}
	// Bit 0 is multicast/group address.
	return (b & 0x01) != 0
}

// IsLocallyAdministeredMAC reports whether the U/L bit (bit 1 of first octet) is set,
// which typically indicates virtual NICs, Docker bridges, veth pairs, or randomized MACs.
func IsLocallyAdministeredMAC(mac string) bool {
	norm, err := NormalizeMAC(mac)
	if err != nil {
		return false
	}
	b, err := strconv.ParseUint(norm[0:2], 16, 8)
	if err != nil {
		return false
	}
	return (b & 0x02) != 0
}

// NormalizeIP extracts a canonical IPv4 or IPv6 string (stripping any /prefixlen).
func NormalizeIP(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "/") {
		if pfx, err := netip.ParsePrefix(s); err == nil {
			return pfx.Addr().Unmap().String()
		}
	}
	if addr, err := netip.ParseAddr(s); err == nil {
		return addr.Unmap().String()
	}
	return ""
}

// NormalizeCIDR normalizes an interface address such as "192.168.1.10/24" while preserving the host bits.
func NormalizeCIDR(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "/") {
		pfx, err := netip.ParsePrefix(s)
		if err != nil {
			return s
		}
		addr := pfx.Addr().Unmap()
		return netip.PrefixFrom(addr, pfx.Bits()).String()
	}
	if addr, err := netip.ParseAddr(s); err == nil {
		addr = addr.Unmap()
		bits := 32
		if addr.Is6() {
			bits = 128
		}
		return netip.PrefixFrom(addr, bits).String()
	}
	return s
}

// NormalizePrefix normalizes a network prefix such as "192.168.1.5/24" -> "192.168.1.0/24".
func NormalizePrefix(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	pfx, err := netip.ParsePrefix(s)
	if err != nil {
		return s
	}
	return pfx.Masked().String()
}

// NormalizeSerial cleans hardware serial numbers and filters BIOS/OEM placeholders.
func NormalizeSerial(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if _, junk := junkSerialList[s]; junk {
		return ""
	}
	if len(s) < 3 {
		return ""
	}
	// Reject strings consisting of a single repeated character like "0000" or "XXXX".
	allSame := true
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return ""
	}
	return s
}

// NormalizeUUID cleans system UUIDs and filters known zero/default placeholder UUIDs.
func NormalizeUUID(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if reUUIDCompact.MatchString(s) {
		s = fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
	}
	if _, junk := junkUUIDList[s]; junk {
		return ""
	}
	if !reUUID.MatchString(s) {
		return ""
	}
	return s
}

// NormalizeSoftName lowers and strips non-alphanumeric chars for fuzzy hostname comparison.
func NormalizeSoftName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	// If FQDN, keep full and short forms matched consistently by stripping domain only when comparing short names.
	return reNonAlnum.ReplaceAllString(s, "")
}

// ShortHostname returns the lowercase short hostname (dropping DNS domain suffix if not an IP).
func ShortHostname(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if _, err := netip.ParseAddr(s); err == nil {
		return s
	}
	if idx := strings.IndexByte(s, '.'); idx > 0 {
		return s[:idx]
	}
	return s
}

// Slugify converts a human label into a URL/YAML-safe lowercase slug.
func Slugify(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = reNonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// PortCandidates maps common switch/router/NIC interface names to canonical candidates such as "Port N".
func PortCandidates(ifName, ifAlias, ifDescr string) []string {
	var candidates []string
	seen := make(map[string]struct{})
	add := func(val string) {
		if val == "" {
			return
		}
		if _, ok := seen[val]; ok {
			return
		}
		seen[val] = struct{}{}
		candidates = append(candidates, val)
	}

	for _, raw := range []string{ifAlias, ifName, ifDescr} {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		if m := rePortN.FindStringSubmatch(text); m != nil {
			n, _ := strconv.Atoi(m[1])
			add(fmt.Sprintf("Port %d", n))
		}
		if m := reSlashPort.FindStringSubmatch(text); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n > 0 {
				add(fmt.Sprintf("Port %d", n))
			}
		}
		if m := reVendorPort.FindStringSubmatch(text); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n > 0 {
				add(fmt.Sprintf("Port %d", n))
			}
		}
		if m := reEthPort.FindStringSubmatch(text); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n == 0 {
				add("Port 1")
			} else {
				add(fmt.Sprintf("Port %d", n))
			}
		}
		if m := reSwpPort.FindStringSubmatch(text); m != nil {
			n, _ := strconv.Atoi(m[1])
			add(fmt.Sprintf("Port %d", n+1))
		}
	}
	return candidates
}

// UnifiPortCandidate translates UniFi OS switch LLDP ethN names (0-indexed) to Port N+1.
func UnifiPortCandidate(raw string) string {
	text := strings.TrimSpace(raw)
	if m := reEthPort.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		return fmt.Sprintf("Port %d", n+1)
	}
	return ""
}

// PortSortNumber extracts the first integer in a port name for natural sorting.
func PortSortNumber(name string) int {
	m := reDigits.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
