package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// UIEvent is broadcast to connected browser clients for live UI updates.
type UIEvent struct {
	Type       string    `json:"type"`
	SnapshotID int64     `json:"snapshot_id,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	Detail     string    `json:"detail,omitempty"`
}

// UIHub manages read-only WebSocket subscriptions for frontend live updates.
type UIHub struct {
	log     *slog.Logger
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func NewUIHub(log *slog.Logger) *UIHub {
	return &UIHub{
		log:     log,
		clients: make(map[chan []byte]struct{}),
	}
}

func (h *UIHub) Broadcast(ev UIEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- b:
		default:
		}
	}
}

func (h *UIHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ch := make(chan []byte, 32)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
		close(ch)
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Drain incoming frames until close (read-only channel).
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				cancel()
				return
			}
		}
	}()

	pingTicker := time.NewTicker(25 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case data := <-ch:
			wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(wctx, websocket.MessageText, data)
			wcancel()
			if err != nil {
				return
			}
		case <-pingTicker.C:
			pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}
