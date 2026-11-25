package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/blalange/auto-netviz/internal/collector/config"
	"github.com/blalange/auto-netviz/internal/collector/sources"
	"github.com/blalange/auto-netviz/internal/identity"
	"github.com/blalange/auto-netviz/internal/protocol"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Source struct {
	Profiles []config.KubernetesProfile
}

func (s *Source) Name() string { return "kubernetes" }

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

	for _, p := range s.Profiles {
		if !config.IsEnabled(p.Enabled) {
			continue
		}
		start := time.Now()
		site := p.Site
		if site == "" {
			site = rt.Site
		}
		clusterName := p.Cluster
		if clusterName == "" {
			clusterName = p.Name
		}
		h := protocol.SourceHealthPayload{Source: "kubernetes", Profile: p.Name, Enabled: true, LastRunAt: &start}

		kubeCfgPath, _ := p.Kubeconfig.Resolve(ctx, rt.BaseDir, rt.K8s, "")
		talosCfgPath, _ := p.TalosConfig.Resolve(ctx, rt.BaseDir, rt.K8s, "")

		var restCfg *rest.Config
		var err error
		if p.InCluster {
			restCfg, err = rest.InClusterConfig()
		} else if kubeCfgPath != "" {
			restCfg, err = clientcmd.BuildConfigFromFlags("", kubeCfgPath)
		} else {
			restCfg, err = rest.InClusterConfig()
		}
		if err != nil {
			h.Status = "error"
			h.Error = fmt.Sprintf("k8s config: %v", err)
			h.DurationMs = time.Since(start).Milliseconds()
			healths = append(healths, h)
			continue
		}

		client, err := k8s.NewForConfig(restCfg)
		if err != nil {
			h.Status = "error"
			h.Error = err.Error()
			h.DurationMs = time.Since(start).Milliseconds()
			healths = append(healths, h)
			continue
		}

		nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			h.Status = "error"
			h.Error = fmt.Sprintf("list nodes: %v", err)
			h.DurationMs = time.Since(start).Milliseconds()
			healths = append(healths, h)
			continue
		}

		var devs []protocol.DeviceObservation
		for _, node := range nodes.Items {
			dev := nodeToObservation(ctx, node, site, clusterName, p.TalosCtl, talosCfgPath)
			devs = append(devs, dev)
		}

		batches = append(batches, protocol.ObservationBatch{
			BatchID:      fmt.Sprintf("k8s-%s-%d", p.Name, time.Now().UnixNano()),
			CollectorID:  rt.CollectorID,
			Site:         site,
			Source:       "kubernetes",
			Profile:      p.Name,
			CollectedAt:  time.Now().UTC(),
			TTLSeconds:   300,
			FullSnapshot: true,
			Devices:      devs,
		})

		done := time.Now()
		h.Status = "ok"
		h.LastSuccessAt = &done
		h.DurationMs = time.Since(start).Milliseconds()
		h.DeviceCount = len(devs)
		healths = append(healths, h)
	}
	return batches, healths, nil
}

func nodeToObservation(ctx context.Context, node corev1.Node, site, cluster, talosCtl, talosCfg string) protocol.DeviceObservation {
	name := node.Name
	info := node.Status.NodeInfo
	role := "worker"
	if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
		role = "control-plane"
	}

	ready := false
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
			ready = true
			break
		}
	}
	status := "offline"
	if ready {
		status = "active"
	}

	var internalIP string
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			internalIP = addr.Address
			break
		}
	}

	sysUUID := identity.NormalizeUUID(node.Status.NodeInfo.SystemUUID)
	platform := "kubernetes"
	if strings.Contains(strings.ToLower(info.OSImage), "talos") {
		platform = "talos-linux"
	}

	var ifaces []protocol.InterfaceObservation
	var macs []string
	var modules []protocol.ModuleObservation

	if talosCtl != "" && talosCfg != "" && internalIP != "" {
		linkMACs := queryTalosLinks(ctx, talosCtl, talosCfg, internalIP)
		for ifName, mac := range linkMACs {
			macs = append(macs, mac)
			iface := protocol.InterfaceObservation{
				Name: ifName, Type: "1000base-t", Kind: "physical", MAC: mac,
			}
			if ifName == "eth0" || ifName == "bond0" {
				iface.IPAddresses = []protocol.IPObservation{{Address: internalIP + "/24", DNSName: name, Primary: true}}
			}
			ifaces = append(ifaces, iface)
		}
		modules = append(modules, queryTalosModules(ctx, talosCtl, talosCfg, internalIP)...)
	}

	if len(ifaces) == 0 && internalIP != "" {
		ifaces = append(ifaces, protocol.InterfaceObservation{
			Name:        "eth0",
			Type:        "1000base-t",
			Kind:        "physical",
			IPAddresses: []protocol.IPObservation{{Address: internalIP + "/24", DNSName: name, Primary: true}},
		})
	}

	claims := protocol.IdentityClaims{
		MACs:      macs,
		Hostnames: []string{name},
		Locators: map[string]string{
			"k8s_node": name,
			"cluster":  cluster,
		},
	}
	if sysUUID != "" {
		claims.UUIDs = []string{sysUUID}
	}
	if internalIP != "" {
		claims.Locators["mgmt_ip"] = internalIP
	}

	cpuQty := node.Status.Capacity.Cpu().Value()
	memGiB := float64(node.Status.Capacity.Memory().Value()) / (1024 * 1024 * 1024)

	return protocol.DeviceObservation{
		ObsKey:        cluster + "/" + name,
		Site:          site,
		Cluster:       cluster,
		Name:          name,
		RoleHint:      role,
		Platform:      platform,
		Status:        status,
		SystemUUID:    sysUUID,
		OSVersion:     info.OSImage,
		KernelVersion: info.KernelVersion,
		PrimaryIP:     internalIP,
		Description:   fmt.Sprintf("%s, Kubernetes %s, %d CPU, %.1f GiB RAM", info.OSImage, info.KubeletVersion, cpuQty, memGiB),
		Claims:        claims,
		Interfaces:    ifaces,
		Modules:       modules,
		Attributes: map[string]any{
			"kubelet_version": info.KubeletVersion,
			"cpu_cores":       cpuQty,
			"memory_gib":      memGiB,
		},
	}
}

func queryTalosLinks(ctx context.Context, bin, cfg, endpoint string) map[string]string {
	outMap := make(map[string]string)
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(cctx, bin, "--talosconfig", cfg, "-n", endpoint, "get", "links", "-o", "json").Output()
	if err != nil {
		return outMap
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var doc struct {
			Metadata struct {
				ID string `json:"id"`
			} `json:"metadata"`
			Spec struct {
				HardwareAddr string `json:"hardwareAddr"`
				Kind         string `json:"kind"`
			} `json:"spec"`
		}
		if json.Unmarshal([]byte(line), &doc) == nil && doc.Metadata.ID != "" && doc.Spec.HardwareAddr != "" {
			if norm, err := identity.NormalizeMAC(doc.Spec.HardwareAddr); err == nil && !identity.IsInvalidMAC(norm) {
				outMap[doc.Metadata.ID] = norm
			}
		}
	}
	return outMap
}

func queryTalosModules(ctx context.Context, bin, cfg, endpoint string) []protocol.ModuleObservation {
	var mods []protocol.ModuleObservation
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(cctx, bin, "--talosconfig", cfg, "-n", endpoint, "get", "disks", "-o", "json").Output()
	if err != nil {
		return mods
	}
	diskIdx := 0
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var doc struct {
			Spec struct {
				Size       int64  `json:"size"`
				Model      string `json:"model"`
				Serial     string `json:"serial"`
				Rotational bool   `json:"rotational"`
				Transport  string `json:"transport"`
			} `json:"spec"`
		}
		if json.Unmarshal([]byte(line), &doc) == nil && doc.Spec.Size > 0 {
			diskIdx++
			sizeGB := int(doc.Spec.Size / 1_000_000_000)
			kind := "SSD"
			if strings.EqualFold(doc.Spec.Transport, "nvme") {
				kind = "NVME"
			} else if doc.Spec.Rotational {
				kind = "HD"
			}
			mods = append(mods, protocol.ModuleObservation{
				Bay:         fmt.Sprintf("Drive %d", diskIdx),
				BayType:     "drive-2-5",
				Profile:     "Hard disk",
				Model:       strings.TrimSpace(doc.Spec.Model),
				Serial:      identity.NormalizeSerial(doc.Spec.Serial),
				Status:      "active",
				Description: fmt.Sprintf("%d GB %s", sizeGB, kind),
			})
		}
	}
	return mods
}
