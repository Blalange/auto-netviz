package db

import (
	"context"
	"testing"
	"time"

	"github.com/blalange/auto-netviz/internal/model"
	"github.com/blalange/auto-netviz/internal/protocol"
)

func TestStoreMigrationsTimeMachineAndSearch(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	state1 := &model.CanonicalState{
		GeneratedAt: time.Now().UTC(),
		Sites:       []model.Site{{ID: "hq", Name: "HQ", Status: "active", Sources: []string{"manual"}}},
		Devices: []model.Device{
			{
				ID:         "dev-1",
				Name:       "core-sw-01",
				Origin:     "manual",
				Site:       "hq",
				Role:       "switch",
				Status:     "active",
				PrimaryIP:  "10.0.0.2",
				Sources:    []string{"manual", "snmp"},
				Interfaces: []model.Interface{{ID: "dev-1:Port 1", Name: "Port 1", Type: "1000base-t", Enabled: true}},
			},
		},
	}

	snap1, changed, err := store.CommitSnapshot(ctx, state1, "startup", "initial")
	if err != nil || !changed || snap1 != 1 {
		t.Fatalf("CommitSnapshot 1 unexpected: snap=%d changed=%v err=%v", snap1, changed, err)
	}

	// Identical state should deduplicate and not insert a second snapshot.
	snapDup, changedDup, err := store.CommitSnapshot(ctx, state1, "poll", "noop")
	if err != nil || changedDup || snapDup != snap1 {
		t.Fatalf("expected deduplication: snap=%d changed=%v err=%v", snapDup, changedDup, err)
	}

	// Mutate state -> snap2
	state2 := &model.CanonicalState{
		GeneratedAt: time.Now().UTC(),
		Sites:       state1.Sites,
		Devices: []model.Device{
			{
				ID:         "dev-1",
				Name:       "core-sw-01",
				Origin:     "manual",
				Site:       "hq",
				Role:       "switch",
				Status:     "offline",
				PrimaryIP:  "10.0.0.2",
				Sources:    []string{"manual", "snmp"},
				Interfaces: state1.Devices[0].Interfaces,
			},
			{
				ID:      "dev-2",
				Name:    "auto-host-01",
				Origin:  "auto",
				Site:    "hq",
				Role:    "server",
				Status:  "active",
				Sources: []string{"discovery"},
			},
		},
	}

	snap2, changed2, err := store.CommitSnapshot(ctx, state2, "collector", "discovery")
	if err != nil || !changed2 || snap2 != 2 {
		t.Fatalf("CommitSnapshot 2 unexpected: snap=%d changed=%v err=%v", snap2, changed2, err)
	}

	if err := store.MaterializeState(ctx, state2); err != nil {
		t.Fatalf("MaterializeState failed: %v", err)
	}

	// Search via FTS5
	results, err := store.Search(ctx, "core", "", 10)
	if err != nil || len(results) == 0 || results[0].Title != "core-sw-01" {
		t.Fatalf("Search unexpected: results=%+v err=%v", results, err)
	}

	// Load historical snap1
	loaded1, sum1, err := store.LoadStateAt(ctx, "1")
	if err != nil || sum1 == nil || len(loaded1.Devices) != 1 || loaded1.Devices[0].Status != "active" {
		t.Fatalf("LoadStateAt(1) unexpected: err=%v sum=%+v devs=%+v", err, sum1, loaded1.Devices)
	}

	// Diff snap1 -> snap2
	diff, err := store.DiffSnapshots(ctx, "1", "2")
	if err != nil {
		t.Fatalf("DiffSnapshots failed: %v", err)
	}
	if diff.AddedCount != 1 || diff.ModifiedCount != 1 {
		t.Fatalf("unexpected diff counts: added=%d modified=%d removed=%d", diff.AddedCount, diff.ModifiedCount, diff.RemovedCount)
	}

	// Raw observation upsert + full_snapshot deactivation
	err = store.SaveObservationBatch(ctx, protocol.ObservationBatch{
		CollectorID:  "c1",
		Site:         "hq",
		Source:       "snmp",
		Profile:      "default",
		FullSnapshot: true,
		Devices: []protocol.DeviceObservation{
			{ObsKey: "10.0.0.2", Name: "core-sw-01"},
		},
	})
	if err != nil {
		t.Fatalf("SaveObservationBatch failed: %v", err)
	}
	obs, err := store.LoadActiveObservations(ctx)
	if err != nil || len(obs) != 1 {
		t.Fatalf("LoadActiveObservations unexpected: len=%d err=%v", len(obs), err)
	}
}
