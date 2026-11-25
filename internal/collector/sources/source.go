package sources

import (
	"context"
	"log/slog"
	"sync"

	"github.com/blalange/auto-netviz/internal/collector/config"
	"github.com/blalange/auto-netviz/internal/protocol"
)

// SharedTargets allows UniFi and Active Discovery to pass newly observed endpoints
// (such as active UniFi switch IPs or newly discovered Redfish/SNMP hosts) to protocol pollers.
type SharedTargets struct {
	mu     sync.RWMutex
	unifi  []UniFiDiscoveredTarget
	probed []ProbedEndpoint
}

type UniFiDiscoveredTarget struct {
	Site           string
	Name           string
	IP             string
	MAC            string
	Serial         string
	SNMPCredential string
}

type ProbedEndpoint struct {
	Site       string
	IP         string
	HasSNMP    bool
	HasRedfish bool
	HasIPMI    bool
	HasSSH     bool
}

func (st *SharedTargets) SetUniFi(items []UniFiDiscoveredTarget) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.unifi = append([]UniFiDiscoveredTarget(nil), items...)
}

func (st *SharedTargets) UniFi() []UniFiDiscoveredTarget {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return append([]UniFiDiscoveredTarget(nil), st.unifi...)
}

func (st *SharedTargets) SetProbed(items []ProbedEndpoint) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.probed = append([]ProbedEndpoint(nil), items...)
}

func (st *SharedTargets) Probed() []ProbedEndpoint {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return append([]ProbedEndpoint(nil), st.probed...)
}

// Runtime provides context to each source plugin.
type Runtime struct {
	CollectorID string
	Site        string
	Zone        string
	BaseDir     string
	Config      *config.Config
	K8s         *config.K8sResolver
	Hints       []protocol.TargetHint
	Shared      *SharedTargets
	Log         *slog.Logger
}

// Runner is implemented by every collector source adapter.
type Runner interface {
	Name() string
	Enabled() bool
	Collect(ctx context.Context, rt Runtime) ([]protocol.ObservationBatch, []protocol.SourceHealthPayload, error)
}
