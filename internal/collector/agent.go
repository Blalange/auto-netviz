package collector

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/blalange/auto-netviz/internal/collector/config"
	"github.com/blalange/auto-netviz/internal/collector/sources"
	"github.com/blalange/auto-netviz/internal/collector/sources/discovery"
	"github.com/blalange/auto-netviz/internal/collector/sources/hostos"
	"github.com/blalange/auto-netviz/internal/collector/sources/ipmi"
	"github.com/blalange/auto-netviz/internal/collector/sources/kubernetes"
	"github.com/blalange/auto-netviz/internal/collector/sources/lldp"
	"github.com/blalange/auto-netviz/internal/collector/sources/redfish"
	"github.com/blalange/auto-netviz/internal/collector/sources/snmp"
	"github.com/blalange/auto-netviz/internal/collector/sources/unifi"
	"github.com/blalange/auto-netviz/internal/protocol"
	"github.com/coder/websocket"
)

type Agent struct {
	cfg       *config.Config
	baseDir   string
	runners   []sources.Runner
	shared    *sources.SharedTargets
	k8s       *config.K8sResolver
	log       *slog.Logger
	startedAt time.Time
	once      bool
	demo      bool

	hintsMu sync.RWMutex
	hints   []protocol.TargetHint

	healthMu sync.RWMutex
	health   map[string]protocol.SourceHealthPayload

	spoolMu sync.Mutex
	spool   []protocol.ObservationBatch

	triggerCh chan struct{}
	sendCh    chan protocol.ObservationBatch
}

func NewAgent(cfg *config.Config, configPath string, once, demo bool, log *slog.Logger) *Agent {
	baseDir := ""
	if configPath != "" {
		baseDir = filepath.Dir(configPath)
	}

	all := []sources.Runner{
		&unifi.Source{Profiles: cfg.Sources.Unifi, Defaults: cfg.Defaults},
		&discovery.Source{Profiles: cfg.Sources.Discovery},
		&snmp.Source{Profiles: cfg.Sources.SNMP},
		&lldp.Source{Profiles: cfg.Sources.LLDP},
		&redfish.Source{Profiles: cfg.Sources.Redfish},
		&ipmi.Source{Profiles: cfg.Sources.IPMI},
		&hostos.Source{Profiles: cfg.Sources.HostOS},
		&kubernetes.Source{Profiles: cfg.Sources.Kubernetes},
	}
	var enabled []sources.Runner
	for _, r := range all {
		if r.Enabled() {
			enabled = append(enabled, r)
		}
	}

	a := &Agent{
		cfg:       cfg,
		baseDir:   baseDir,
		runners:   enabled,
		shared:    &sources.SharedTargets{},
		k8s:       &config.K8sResolver{},
		log:       log,
		startedAt: time.Now().UTC(),
		once:      once,
		demo:      demo,
		health:    make(map[string]protocol.SourceHealthPayload),
		triggerCh: make(chan struct{}, 4),
		sendCh:    make(chan protocol.ObservationBatch, 128),
	}
	a.loadSpool()
	return a
}

func (a *Agent) Run(ctx context.Context) error {
	if a.once {
		return a.runOnce(ctx)
	}

	// Start source collection loop
	go a.schedulerLoop(ctx)

	// Outbound WebSocket connection loop with exponential backoff + jitter
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		connStart := time.Now()
		err := a.connectAndServe(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(connStart) > 30*time.Second {
			backoff = time.Second
		}
		jitterMs, _ := rand.Int(rand.Reader, big.NewInt(1000))
		wait := backoff + time.Duration(jitterMs.Int64())*time.Millisecond
		a.log.Warn("collector ws disconnected, reconnecting", "wait", wait, "err", err)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		backoff *= 2
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
	}
}

func (a *Agent) runOnce(ctx context.Context) error {
	connCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(connCtx, a.cfg.ServerWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial server ws %s: %w", a.cfg.ServerWSURL, err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 * 1024 * 1024)

	if err := a.sendHello(connCtx, conn); err != nil {
		return err
	}

	// Read hello_ack and initial target_hints
	hintDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(hintDeadline) {
		rCtx, rCancel := context.WithTimeout(connCtx, time.Until(hintDeadline))
		_, raw, err := conn.Read(rCtx)
		rCancel()
		if err != nil {
			break
		}
		var env protocol.Envelope
		if json.Unmarshal(raw, &env) != nil {
			continue
		}
		if env.Type == protocol.MsgTargetHints {
			var hp protocol.TargetHintsPayload
			if json.Unmarshal(env.Payload, &hp) == nil {
				a.setHints(hp.Hints)
			}
			break
		}
	}

	batches := a.collectAllOnce(ctx)
	for _, b := range batches {
		if err := a.writeEnvelope(connCtx, conn, protocol.MsgObservationBatch, b); err != nil {
			return err
		}
		_, ackRaw, err := conn.Read(connCtx)
		if err != nil {
			return err
		}
		a.log.Debug("received ack", "raw", string(ackRaw))
	}
	_ = a.sendHeartbeat(connCtx, conn)
	_ = conn.Close(websocket.StatusNormalClosure, "once completed")
	return nil
}

func (a *Agent) connectAndServe(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, a.cfg.ServerWSURL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 * 1024 * 1024)

	if err := a.sendHello(ctx, conn); err != nil {
		return err
	}
	a.log.Info("connected to auto-netviz server", "url", a.cfg.ServerWSURL, "collector_id", a.cfg.CollectorID)

	// Drain any spooled batches before live sendCh
	a.drainSpoolToSendCh()

	sessionCtx, sessionCancel := context.WithCancel(ctx)
	defer sessionCancel()

	errCh := make(chan error, 2)

	// Reader goroutine
	go func() {
		for {
			_, raw, err := conn.Read(sessionCtx)
			if err != nil {
				errCh <- err
				return
			}
			var env protocol.Envelope
			if json.Unmarshal(raw, &env) != nil {
				continue
			}
			switch env.Type {
			case protocol.MsgTargetHints:
				var hp protocol.TargetHintsPayload
				if json.Unmarshal(env.Payload, &hp) == nil {
					a.setHints(hp.Hints)
					a.log.Info("received updated target hints", "count", len(hp.Hints))
					select {
					case a.triggerCh <- struct{}{}:
					default:
					}
				}
			case protocol.MsgTriggerCollect:
				select {
				case a.triggerCh <- struct{}{}:
				default:
				}
			case protocol.MsgObservationAck:
				var ack protocol.ObservationAckPayload
				if json.Unmarshal(env.Payload, &ack) == nil {
					if ack.Error != "" {
						a.log.Warn("server rejected observation batch", "batch", ack.BatchID, "err", ack.Error)
					} else {
						a.log.Debug("observation batch acked", "batch", ack.BatchID, "snapshot", ack.SnapshotID, "changed", ack.Changed)
					}
				}
			}
		}
	}()

	// Writer + heartbeat goroutine
	hbInterval := config.ParseDuration(a.cfg.HeartbeatInterval, 15*time.Second)
	go func() {
		hbTicker := time.NewTicker(hbInterval)
		defer hbTicker.Stop()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case batch := <-a.sendCh:
				if err := a.writeEnvelope(sessionCtx, conn, protocol.MsgObservationBatch, batch); err != nil {
					a.enqueueSpool(batch)
					errCh <- err
					return
				}
			case <-hbTicker.C:
				if err := a.sendHeartbeat(sessionCtx, conn); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	// Initial trigger on connect
	select {
	case a.triggerCh <- struct{}{}:
	default:
	}

	select {
	case <-ctx.Done():
		_ = conn.Close(websocket.StatusNormalClosure, "shutting down")
		return nil
	case err := <-errCh:
		return err
	}
}

func (a *Agent) schedulerLoop(ctx context.Context) {
	// Run an immediate cycle on boot, then tick every 15s checking per-source schedule due times.
	lastRun := make(map[string]time.Time)
	checkAndRun := func(force bool) {
		if a.demo {
			if force || time.Since(lastRun["demo"]) >= 30*time.Second {
				lastRun["demo"] = time.Now()
				for _, b := range BuildDemoBatches(a.cfg.CollectorID, a.cfg.Site) {
					a.submitBatch(b)
				}
				a.recordHealth(DemoHealth())
			}
			return
		}

		// Run in runner order so UniFi/Discovery populate SharedTargets before SNMP/Redfish/IPMI.
		for _, r := range a.runners {
			sched := config.ParseDuration(a.cfg.Schedules[r.Name()], 60*time.Second)
			if !force && !lastRun[r.Name()].IsZero() && time.Since(lastRun[r.Name()]) < sched {
				continue
			}
			lastRun[r.Name()] = time.Now()
			batches, healths, err := r.Collect(ctx, a.runtime())
			if err != nil {
				a.log.Warn("source collect error", "source", r.Name(), "err", err)
			}
			a.recordHealth(healths)
			for _, b := range batches {
				a.submitBatch(b)
			}
		}
	}

	checkAndRun(true)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.triggerCh:
			checkAndRun(true)
		case <-ticker.C:
			checkAndRun(false)
		}
	}
}

func (a *Agent) collectAllOnce(ctx context.Context) []protocol.ObservationBatch {
	if a.demo {
		a.recordHealth(DemoHealth())
		return BuildDemoBatches(a.cfg.CollectorID, a.cfg.Site)
	}
	var all []protocol.ObservationBatch
	for _, r := range a.runners {
		batches, healths, err := r.Collect(ctx, a.runtime())
		if err != nil {
			a.log.Warn("source collect error", "source", r.Name(), "err", err)
		}
		a.recordHealth(healths)
		all = append(all, batches...)
	}
	return all
}

func (a *Agent) runtime() sources.Runtime {
	a.hintsMu.RLock()
	hints := append([]protocol.TargetHint(nil), a.hints...)
	a.hintsMu.RUnlock()

	return sources.Runtime{
		CollectorID: a.cfg.CollectorID,
		Site:        a.cfg.Site,
		Zone:        a.cfg.Zone,
		BaseDir:     a.baseDir,
		Config:      a.cfg,
		K8s:         a.k8s,
		Hints:       hints,
		Shared:      a.shared,
		Log:         a.log,
	}
}

func (a *Agent) sendHello(ctx context.Context, conn *websocket.Conn) error {
	srcNames := make([]string, 0, len(a.runners))
	if a.demo {
		srcNames = []string{"unifi", "snmp", "lldp", "redfish", "ipmi", "discovery", "hostos", "kubernetes"}
	} else {
		for _, r := range a.runners {
			srcNames = append(srcNames, r.Name())
		}
	}
	host, _ := os.Hostname()
	return a.writeEnvelope(ctx, conn, protocol.MsgHello, protocol.HelloPayload{
		CollectorID: a.cfg.CollectorID,
		Name:        a.cfg.Name,
		Site:        a.cfg.Site,
		Zone:        a.cfg.Zone,
		Version:     "0.1.0",
		Hostname:    host,
		Sources:     srcNames,
	})
}

func (a *Agent) sendHeartbeat(ctx context.Context, conn *websocket.Conn) error {
	a.healthMu.RLock()
	srcs := make([]protocol.SourceHealthPayload, 0, len(a.health))
	for _, h := range a.health {
		srcs = append(srcs, h)
	}
	a.healthMu.RUnlock()

	a.spoolMu.Lock()
	qDepth := len(a.spool) + len(a.sendCh)
	a.spoolMu.Unlock()

	return a.writeEnvelope(ctx, conn, protocol.MsgHeartbeat, protocol.HeartbeatPayload{
		CollectorID:   a.cfg.CollectorID,
		UptimeSeconds: int64(time.Since(a.startedAt).Seconds()),
		QueueDepth:    qDepth,
		Sources:       srcs,
	})
}

func (a *Agent) writeEnvelope(ctx context.Context, conn *websocket.Conn, msgType string, payload any) error {
	env, err := protocol.NewEnvelope(msgType, a.cfg.CollectorID, payload)
	if err != nil {
		return err
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

func (a *Agent) setHints(hints []protocol.TargetHint) {
	a.hintsMu.Lock()
	a.hints = hints
	a.hintsMu.Unlock()
}

func (a *Agent) recordHealth(items []protocol.SourceHealthPayload) {
	a.healthMu.Lock()
	defer a.healthMu.Unlock()
	for _, it := range items {
		a.health[it.Source+":"+it.Profile] = it
	}
}

func (a *Agent) submitBatch(b protocol.ObservationBatch) {
	select {
	case a.sendCh <- b:
	default:
		a.enqueueSpool(b)
	}
}

func (a *Agent) spoolFile() string {
	if a.cfg.SpoolDir == "" {
		return ""
	}
	return filepath.Join(a.cfg.SpoolDir, "queue.jsonl")
}

func (a *Agent) enqueueSpool(b protocol.ObservationBatch) {
	a.spoolMu.Lock()
	defer a.spoolMu.Unlock()
	if len(a.spool) >= 200 {
		a.spool = a.spool[1:]
	}
	a.spool = append(a.spool, b)
	a.persistSpoolLocked()
}

func (a *Agent) drainSpoolToSendCh() {
	a.spoolMu.Lock()
	pending := append([]protocol.ObservationBatch(nil), a.spool...)
	a.spool = nil
	a.persistSpoolLocked()
	a.spoolMu.Unlock()

	for _, b := range pending {
		a.submitBatch(b)
	}
}

func (a *Agent) loadSpool() {
	sf := a.spoolFile()
	if sf == "" {
		return
	}
	f, err := os.Open(sf)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var b protocol.ObservationBatch
		if json.Unmarshal(sc.Bytes(), &b) == nil {
			a.spool = append(a.spool, b)
		}
	}
}

func (a *Agent) persistSpoolLocked() {
	sf := a.spoolFile()
	if sf == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(sf), 0o755)
	if len(a.spool) == 0 {
		_ = os.Remove(sf)
		return
	}
	f, err := os.Create(sf)
	if err != nil {
		return
	}
	defer f.Close()
	for _, b := range a.spool {
		if raw, err := json.Marshal(b); err == nil {
			_, _ = f.Write(raw)
			_, _ = f.Write([]byte{'\n'})
		}
	}
}
