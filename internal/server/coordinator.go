package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/blalange/auto-netviz/internal/db"
	"github.com/blalange/auto-netviz/internal/manual"
	"github.com/blalange/auto-netviz/internal/merge"
	"github.com/blalange/auto-netviz/internal/model"
	"github.com/blalange/auto-netviz/internal/protocol"
)

// Coordinator owns the manual inventory reload cycle, serializes merge + snapshot
// commits, and notifies connected collectors and UI browsers.
type Coordinator struct {
	store *db.Store
	log   *slog.Logger

	reconcileMu sync.Mutex
	stateMu     sync.RWMutex
	inv         *manual.Inventory
	state       *model.CanonicalState
	latestSnap  *model.SnapshotSummary

	collectorHub *CollectorHub
	uiHub        *UIHub
}

func NewCoordinator(store *db.Store, inv *manual.Inventory, log *slog.Logger) *Coordinator {
	c := &Coordinator{
		store: store,
		inv:   inv,
		log:   log,
	}
	c.collectorHub = NewCollectorHub(c, log)
	c.uiHub = NewUIHub(log)
	return c
}

func (c *Coordinator) CollectorHub() *CollectorHub {
	return c.collectorHub
}

func (c *Coordinator) UIHub() *UIHub {
	return c.uiHub
}

func (c *Coordinator) CurrentState() (*model.CanonicalState, *model.SnapshotSummary) {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state, c.latestSnap
}

func (c *Coordinator) ManualInventory() *manual.Inventory {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.inv
}

// Reconcile loads active observations from SQLite, merges with manual YAML,
// materializes current tables, commits a Time Machine snapshot if changed, and broadcasts UI updates.
func (c *Coordinator) Reconcile(ctx context.Context, trigger, detail string) (int64, bool, error) {
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()

	c.stateMu.RLock()
	inv := c.inv
	c.stateMu.RUnlock()

	rawObs, err := c.store.LoadActiveObservations(ctx)
	if err != nil {
		return 0, false, err
	}
	prevIDs, err := c.store.LoadIdentities(ctx)
	if err != nil {
		return 0, false, err
	}

	res, err := merge.Merge(inv, rawObs, prevIDs)
	if err != nil {
		return 0, false, err
	}

	if err := c.store.UpsertIdentities(ctx, res.Identities); err != nil {
		return 0, false, err
	}

	snapID, changed, err := c.store.CommitSnapshot(ctx, res.State, trigger, detail)
	if err != nil {
		return 0, false, err
	}

	if changed || c.state == nil {
		if err := c.store.MaterializeState(ctx, res.State); err != nil {
			return snapID, changed, err
		}
	}

	sum, _ := c.store.ResolveSnapshot(ctx, "")
	c.stateMu.Lock()
	c.state = res.State
	if sum != nil {
		c.latestSnap = sum
	}
	c.stateMu.Unlock()

	if changed {
		c.log.Info("time machine snapshot committed",
			"snapshot_id", snapID,
			"trigger", trigger,
			"devices", len(res.State.Devices),
			"cables", len(res.State.Cables),
			"issues", len(res.State.Issues),
		)
		c.uiHub.Broadcast(UIEvent{
			Type:       "snapshot_created",
			SnapshotID: snapID,
			Timestamp:  time.Now().UTC(),
			Detail:     detail,
		})
	}
	return snapID, changed, nil
}

// UpdateManual swaps in a newly reloaded manual YAML inventory, pushes updated
// target hints to all connected collectors, and triggers a merge/snapshot.
func (c *Coordinator) UpdateManual(ctx context.Context, inv *manual.Inventory) error {
	if inv == nil {
		return nil
	}
	c.stateMu.Lock()
	c.inv = inv
	c.stateMu.Unlock()

	_ = c.store.RecordManualRevision(ctx, inv.Hash, len(inv.FilesLoaded), len(inv.Issues))
	snapID, _, err := c.Reconcile(ctx, "manual_reload", inv.Dir)
	if err != nil {
		return err
	}

	c.collectorHub.PushTargetHintsAll(inv)
	c.uiHub.Broadcast(UIEvent{
		Type:       "manual_reloaded",
		SnapshotID: snapID,
		Timestamp:  time.Now().UTC(),
		Detail:     inv.Hash,
	})
	return nil
}

func (c *Coordinator) HandleBatch(ctx context.Context, batch protocol.ObservationBatch) (protocol.ObservationAckPayload, error) {
	if err := c.store.SaveObservationBatch(ctx, batch); err != nil {
		return protocol.ObservationAckPayload{BatchID: batch.BatchID, Error: err.Error()}, err
	}
	detail := batch.CollectorID + ":" + batch.Source
	if batch.Profile != "" {
		detail += "/" + batch.Profile
	}
	snapID, changed, err := c.Reconcile(ctx, "collector", detail)
	if err != nil {
		return protocol.ObservationAckPayload{BatchID: batch.BatchID, Error: err.Error()}, err
	}
	return protocol.ObservationAckPayload{
		BatchID:          batch.BatchID,
		SnapshotID:       snapID,
		Changed:          changed,
		ProcessedDevices: len(batch.Devices),
		ProcessedLinks:   len(batch.Links),
	}, nil
}

// StartMaintenance periodically expires stale TTL observations and prunes old snapshots.
func (c *Coordinator) StartMaintenance(ctx context.Context, retentionDays int) {
	ttlTicker := time.NewTicker(60 * time.Second)
	gcTicker := time.NewTicker(6 * time.Hour)
	go func() {
		defer ttlTicker.Stop()
		defer gcTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ttlTicker.C:
				_, _, err := c.Reconcile(ctx, "ttl_sweep", "periodic expiry check")
				if err != nil {
					c.log.Warn("periodic ttl reconcile error", "err", err)
				}
			case <-gcTicker.C:
				if deleted, err := c.store.PruneRetention(ctx, retentionDays); err != nil {
					c.log.Warn("retention prune failed", "err", err)
				} else if deleted > 0 {
					c.log.Info("pruned expired snapshots", "count", deleted)
				}
			}
		}
	}()
}
