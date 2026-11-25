package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/blalange/auto-netviz/internal/db"
	"github.com/blalange/auto-netviz/internal/manual"
	"github.com/blalange/auto-netviz/internal/protocol"
	"github.com/coder/websocket"
)

func TestServerWebSocketCollectorAndReadOnlyTimeMachineAPI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	inv, err := manual.LoadDir("../../examples/manual")
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coord := NewCoordinator(store, inv, logger)
	if err := coord.UpdateManual(ctx, inv); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(NewRouter(coord))
	defer ts.Close()

	// 1. Verify strictly read-only API rejects POST
	respPost, err := http.Post(ts.URL+"/api/v1/devices", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = respPost.Body.Close()
	if respPost.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed on POST, got %d", respPost.StatusCode)
	}

	// 2. Connect simulated collector over WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/collector"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial failed: %v", err)
	}
	defer conn.CloseNow()

	helloEnv, _ := protocol.NewEnvelope(protocol.MsgHello, "col-test-1", protocol.HelloPayload{
		CollectorID: "col-test-1",
		Name:        "Test Collector",
		Site:        "hq",
		Version:     "0.1.0",
		Sources:     []string{"discovery"},
	})
	bHello, _ := json.Marshal(helloEnv)
	if err := conn.Write(ctx, websocket.MessageText, bHello); err != nil {
		t.Fatal(err)
	}

	// Expect hello_ack and target_hints
	for i := 0; i < 2; i++ {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var env protocol.Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatal(err)
		}
		if env.Type == protocol.MsgTargetHints {
			var hints protocol.TargetHintsPayload
			_ = json.Unmarshal(env.Payload, &hints)
			if len(hints.Hints) == 0 {
				t.Fatalf("expected non-empty target hints for hq")
			}
		}
	}

	// 3. Push observation batch adding an auto-discovered device
	batchEnv, _ := protocol.NewEnvelope(protocol.MsgObservationBatch, "col-test-1", protocol.ObservationBatch{
		BatchID:      "b1",
		CollectorID:  "col-test-1",
		Site:         "hq",
		Source:       "discovery",
		Profile:      "hq-lan",
		FullSnapshot: true,
		Devices: []protocol.DeviceObservation{
			{
				ObsKey:    "10.10.10.222",
				Site:      "hq",
				Name:      "auto-printer-222",
				RoleHint:  "appliance",
				PrimaryIP: "10.10.10.222",
				Claims:    protocol.IdentityClaims{MACs: []string{"00:11:32:aa:bb:cc"}},
			},
		},
	})
	bBatch, _ := json.Marshal(batchEnv)
	if err := conn.Write(ctx, websocket.MessageText, bBatch); err != nil {
		t.Fatal(err)
	}

	_, ackRaw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ackEnv protocol.Envelope
	_ = json.Unmarshal(ackRaw, &ackEnv)
	var ack protocol.ObservationAckPayload
	_ = json.Unmarshal(ackEnv.Payload, &ack)
	if !ack.Changed || ack.SnapshotID < 2 {
		t.Fatalf("expected new snapshot after batch, got %+v", ack)
	}

	// 4. Query snapshot 1 (before auto device) vs snapshot 2 (live)
	r1, err := http.Get(ts.URL + "/api/v1/devices?at=1")
	if err != nil {
		t.Fatal(err)
	}
	var p1 struct {
		Devices []any `json:"devices"`
	}
	_ = json.NewDecoder(r1.Body).Decode(&p1)
	_ = r1.Body.Close()

	r2, err := http.Get(ts.URL + "/api/v1/devices")
	if err != nil {
		t.Fatal(err)
	}
	var p2 struct {
		Devices []any `json:"devices"`
	}
	_ = json.NewDecoder(r2.Body).Decode(&p2)
	_ = r2.Body.Close()

	if len(p2.Devices) != len(p1.Devices)+1 {
		t.Fatalf("expected +1 device in live vs snap 1: snap1=%d live=%d", len(p1.Devices), len(p2.Devices))
	}

	// 5. Cable path trace between node-01 and hq-gw-01
	rTrace, err := http.Get(ts.URL + "/api/v1/cables/trace?from=node-01&to=hq-gw-01")
	if err != nil {
		t.Fatal(err)
	}
	var trace struct {
		Found bool  `json:"found"`
		Hops  []any `json:"hops"`
	}
	_ = json.NewDecoder(rTrace.Body).Decode(&trace)
	_ = rTrace.Body.Close()
	if !trace.Found || len(trace.Hops) != 2 {
		t.Fatalf("expected 2-hop trace node-01 -> hq-sw-01 -> hq-gw-01, got %+v", trace)
	}
}
