package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/blalange/auto-netviz/internal/manual"
	"github.com/blalange/auto-netviz/internal/protocol"
	"github.com/coder/websocket"
)

type collectorConn struct {
	collectorID string
	site        string
	sendCh      chan []byte
}

// CollectorHub manages active outbound WebSocket connections from remote Go collectors.
type CollectorHub struct {
	coord *Coordinator
	log   *slog.Logger

	mu    sync.RWMutex
	conns map[string]*collectorConn
}

func NewCollectorHub(coord *Coordinator, log *slog.Logger) *CollectorHub {
	return &CollectorHub{
		coord: coord,
		log:   log,
		conns: make(map[string]*collectorConn),
	}
}

func (h *CollectorHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.log.Warn("collector ws accept failed", "err", err)
		return
	}
	conn.SetReadLimit(16 * 1024 * 1024) // 16 MiB batches
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// First frame must be hello.
	_, rawHello, err := conn.Read(ctx)
	if err != nil {
		return
	}
	var env protocol.Envelope
	if err := json.Unmarshal(rawHello, &env); err != nil || env.Type != protocol.MsgHello {
		h.log.Warn("collector ws expected hello frame", "type", env.Type, "err", err)
		return
	}
	var hello protocol.HelloPayload
	if err := json.Unmarshal(env.Payload, &hello); err != nil || hello.CollectorID == "" {
		h.log.Warn("collector ws invalid hello payload", "err", err)
		return
	}

	if err := h.coord.store.UpsertCollectorHello(ctx, hello); err != nil {
		h.log.Error("failed saving collector hello", "collector", hello.CollectorID, "err", err)
		return
	}

	cc := &collectorConn{
		collectorID: hello.CollectorID,
		site:        hello.Site,
		sendCh:      make(chan []byte, 64),
	}
	h.register(cc)
	defer func() {
		h.unregister(cc)
		_ = h.coord.store.MarkCollectorOffline(context.Background(), hello.CollectorID)
		h.coord.uiHub.Broadcast(UIEvent{
			Type:      "collector_updated",
			Timestamp: time.Now().UTC(),
			Detail:    hello.CollectorID + ":offline",
		})
		h.log.Info("collector disconnected", "collector_id", hello.CollectorID)
	}()

	h.log.Info("collector connected",
		"collector_id", hello.CollectorID,
		"site", hello.Site,
		"zone", hello.Zone,
		"sources", hello.Sources,
	)
	h.coord.uiHub.Broadcast(UIEvent{
		Type:      "collector_updated",
		Timestamp: time.Now().UTC(),
		Detail:    hello.CollectorID + ":online",
	})

	// Send hello_ack + initial target hints.
	h.sendEnvelope(cc, protocol.MsgHelloAck, protocol.HelloAckPayload{
		Accepted:                 true,
		ServerVersion:            "0.1.0",
		HeartbeatIntervalSeconds: 15,
	})
	h.sendHintsToConn(cc, h.coord.ManualInventory())

	// Writer goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-cc.sendCh:
				if !ok {
					return
				}
				wctx, wcancel := context.WithTimeout(ctx, 15*time.Second)
				err := conn.Write(wctx, websocket.MessageText, data)
				wcancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	// Reader loop
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var frame protocol.Envelope
		if err := json.Unmarshal(data, &frame); err != nil {
			h.log.Warn("collector ws malformed envelope", "collector_id", hello.CollectorID, "err", err)
			continue
		}
		switch frame.Type {
		case protocol.MsgObservationBatch:
			var batch protocol.ObservationBatch
			if err := json.Unmarshal(frame.Payload, &batch); err != nil {
				h.log.Warn("invalid observation batch", "collector_id", hello.CollectorID, "err", err)
				continue
			}
			if batch.CollectorID == "" {
				batch.CollectorID = hello.CollectorID
			}
			if batch.Site == "" {
				batch.Site = hello.Site
			}
			ack, err := h.coord.HandleBatch(ctx, batch)
			if err != nil {
				h.log.Error("handle batch failed", "collector_id", hello.CollectorID, "source", batch.Source, "err", err)
			}
			h.sendEnvelope(cc, protocol.MsgObservationAck, ack)

		case protocol.MsgHeartbeat:
			var hb protocol.HeartbeatPayload
			if err := json.Unmarshal(frame.Payload, &hb); err != nil {
				continue
			}
			if hb.CollectorID == "" {
				hb.CollectorID = hello.CollectorID
			}
			if err := h.coord.store.UpdateCollectorHeartbeat(ctx, hb); err != nil {
				h.log.Warn("failed updating collector heartbeat", "collector_id", hello.CollectorID, "err", err)
			}
			h.coord.uiHub.Broadcast(UIEvent{
				Type:      "collector_updated",
				Timestamp: time.Now().UTC(),
				Detail:    hello.CollectorID + ":heartbeat",
			})
		}
	}
}

func (h *CollectorHub) PushTargetHintsAll(inv *manual.Inventory) {
	h.mu.RLock()
	conns := make([]*collectorConn, 0, len(h.conns))
	for _, cc := range h.conns {
		conns = append(conns, cc)
	}
	h.mu.RUnlock()

	for _, cc := range conns {
		h.sendHintsToConn(cc, inv)
	}
}

func (h *CollectorHub) sendHintsToConn(cc *collectorConn, inv *manual.Inventory) {
	if inv == nil {
		return
	}
	hints := inv.BuildTargetHints(cc.collectorID, cc.site)
	h.sendEnvelope(cc, protocol.MsgTargetHints, protocol.TargetHintsPayload{
		Revision: inv.Hash,
		Hints:    hints,
	})
}

func (h *CollectorHub) sendEnvelope(cc *collectorConn, msgType string, payload any) {
	env, err := protocol.NewEnvelope(msgType, cc.collectorID, payload)
	if err != nil {
		return
	}
	b, err := json.Marshal(env)
	if err != nil {
		return
	}
	select {
	case cc.sendCh <- b:
	default:
		h.log.Warn("collector send buffer full, dropping frame", "collector_id", cc.collectorID, "type", msgType)
	}
}

func (h *CollectorHub) register(cc *collectorConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.conns[cc.collectorID]; ok {
		close(old.sendCh)
	}
	h.conns[cc.collectorID] = cc
}

func (h *CollectorHub) unregister(cc *collectorConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.conns[cc.collectorID]; ok && cur == cc {
		delete(h.conns, cc.collectorID)
		close(cc.sendCh)
	}
}
