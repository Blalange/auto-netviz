package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blalange/auto-netviz/internal/model"
	"github.com/klauspost/compress/zstd"
)

var (
	zstdEnc, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	zstdDec, _ = zstd.NewReader(nil)
)

const (
	kindSite       = "site"
	kindLocation   = "location"
	kindOwnerGroup = "owner_group"
	kindOwner      = "owner"
	kindContact    = "contact"
	kindDeviceType = "device_type"
	kindModuleType = "module_type"
	kindRack       = "rack"
	kindDevice     = "device"
	kindVLAN       = "vlan"
	kindPrefix     = "prefix"
	kindCable      = "cable"
	kindAsset      = "asset"
	kindIssue      = "issue"
)

type entityRecord struct {
	kind     string
	id       string
	name     string
	rawJSON  []byte
	blobHash string
}

// CommitSnapshot deduplicates canonical state by state_hash and persists a new
// content-addressed Time Machine snapshot + human-readable diff events if anything changed.
func (s *Store) CommitSnapshot(ctx context.Context, state *model.CanonicalState, trigger, detail string) (int64, bool, error) {
	if state == nil {
		return 0, false, fmt.Errorf("nil canonical state")
	}

	records, stateHash, counts, err := flattenState(state)
	if err != nil {
		return 0, false, err
	}
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var prevID int64
	var prevHash string
	err = tx.QueryRowContext(ctx, `SELECT id, state_hash FROM tm_snapshots ORDER BY id DESC LIMIT 1`).Scan(&prevID, &prevHash)
	if err != nil && err != sql.ErrNoRows {
		return 0, false, err
	}

	if err == nil && prevHash == stateHash {
		if _, err := tx.ExecContext(ctx, `UPDATE tm_snapshots SET last_verified_at = ? WHERE id = ?`, nowStr, prevID); err != nil {
			return 0, false, err
		}
		if err := tx.Commit(); err != nil {
			return 0, false, err
		}
		state.SnapshotID = prevID
		return prevID, false, nil
	}

	prevEntities := make(map[string]string) // kind|id -> blob_hash
	if prevID > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT entity_kind, entity_id, blob_hash FROM tm_snapshot_entities WHERE snapshot_id = ?`, prevID)
		if err != nil {
			return 0, false, err
		}
		for rows.Next() {
			var k, id, h string
			if err := rows.Scan(&k, &id, &h); err != nil {
				_ = rows.Close()
				return 0, false, err
			}
			prevEntities[k+"\x00"+id] = h
		}
		_ = rows.Close()
	}

	summaryBytes, _ := json.Marshal(counts)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO tm_snapshots (created_at, last_verified_at, trigger, trigger_detail, state_hash, summary_json) VALUES (?, ?, ?, ?, ?, ?)`,
		nowStr, nowStr, trigger, detail, stateHash, string(summaryBytes),
	)
	if err != nil {
		return 0, false, err
	}
	snapID, err := res.LastInsertId()
	if err != nil {
		return 0, false, err
	}

	insertBlobStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO tm_blobs (hash, data) VALUES (?, ?)`)
	if err != nil {
		return 0, false, err
	}
	defer insertBlobStmt.Close()

	insertEntityStmt, err := tx.PrepareContext(ctx, `INSERT INTO tm_snapshot_entities (snapshot_id, entity_kind, entity_id, blob_hash) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return 0, false, err
	}
	defer insertEntityStmt.Close()

	currMap := make(map[string]entityRecord, len(records))
	for _, rec := range records {
		currMap[rec.kind+"\x00"+rec.id] = rec
		compressed := zstdEnc.EncodeAll(rec.rawJSON, nil)
		if _, err := insertBlobStmt.ExecContext(ctx, rec.blobHash, compressed); err != nil {
			return 0, false, err
		}
		if _, err := insertEntityStmt.ExecContext(ctx, snapID, rec.kind, rec.id, rec.blobHash); err != nil {
			return 0, false, err
		}
	}

	events := buildTimelineEvents(ctx, tx, snapID, nowStr, trigger, detail, prevID, prevEntities, currMap)
	if len(events) > 0 {
		evStmt, err := tx.PrepareContext(ctx,
			`INSERT INTO tm_events (snapshot_id, occurred_at, event_type, entity_kind, entity_id, entity_name, summary, diff_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		)
		if err != nil {
			return 0, false, err
		}
		defer evStmt.Close()
		for _, ev := range events {
			if _, err := evStmt.ExecContext(ctx, ev.SnapshotID, nowStr, ev.EventType, ev.EntityKind, ev.EntityID, ev.EntityName, ev.Summary, ev.DiffJSON); err != nil {
				return 0, false, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	state.SnapshotID = snapID
	s.cacheSnapshot(snapID, state)
	return snapID, true, nil
}

// ResolveSnapshot finds the snapshot matching atParam (empty = latest, integer = snapshot id, or RFC3339/unix timestamp).
func (s *Store) ResolveSnapshot(ctx context.Context, atParam string) (*model.SnapshotSummary, error) {
	atParam = strings.TrimSpace(atParam)
	var row *sql.Row
	const selectCols = `
		SELECT s.id, s.created_at, s.last_verified_at, s.trigger, s.trigger_detail, s.state_hash, s.summary_json,
		       (SELECT COUNT(*) FROM tm_events e WHERE e.snapshot_id = s.id)
		FROM tm_snapshots s
	`
	if atParam == "" || strings.EqualFold(atParam, "live") {
		row = s.sql.QueryRowContext(ctx, selectCols+` ORDER BY s.id DESC LIMIT 1`)
	} else if id, err := strconv.ParseInt(atParam, 10, 64); err == nil {
		// Heuristic: if >= 1_000_000_000 treat as unix epoch seconds, else snapshot ID.
		if id >= 1_000_000_000 {
			ts := time.Unix(id, 0).UTC().Format(time.RFC3339Nano)
			row = s.sql.QueryRowContext(ctx, selectCols+` WHERE s.created_at <= ? ORDER BY s.id DESC LIMIT 1`, ts)
		} else {
			row = s.sql.QueryRowContext(ctx, selectCols+` WHERE s.id = ?`, id)
		}
	} else if ts, err := time.Parse(time.RFC3339Nano, atParam); err == nil {
		row = s.sql.QueryRowContext(ctx, selectCols+` WHERE s.created_at <= ? ORDER BY s.id DESC LIMIT 1`, ts.UTC().Format(time.RFC3339Nano))
	} else if ts, err := time.Parse(time.RFC3339, atParam); err == nil {
		row = s.sql.QueryRowContext(ctx, selectCols+` WHERE s.created_at <= ? ORDER BY s.id DESC LIMIT 1`, ts.UTC().Format(time.RFC3339Nano))
	} else {
		return nil, fmt.Errorf("invalid 'at' parameter %q", atParam)
	}

	var sum model.SnapshotSummary
	var createdStr, verifiedStr, countsJSON string
	err := row.Scan(&sum.ID, &createdStr, &verifiedStr, &sum.Trigger, &sum.TriggerDetail, &sum.StateHash, &countsJSON, &sum.EventCount)
	if err != nil {
		return nil, err
	}
	sum.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	sum.LastVerifiedAt, _ = time.Parse(time.RFC3339Nano, verifiedStr)
	_ = json.Unmarshal([]byte(countsJSON), &sum.Counts)
	return &sum, nil
}

// LoadStateAt reconstructs the CanonicalState for a historical or current snapshot.
func (s *Store) LoadStateAt(ctx context.Context, atParam string) (*model.CanonicalState, *model.SnapshotSummary, error) {
	sum, err := s.ResolveSnapshot(ctx, atParam)
	if err != nil {
		if err == sql.ErrNoRows {
			return &model.CanonicalState{GeneratedAt: time.Now().UTC()}, nil, nil
		}
		return nil, nil, err
	}

	s.cacheMu.RLock()
	if cached, ok := s.snapshotCache[sum.ID]; ok {
		s.cacheMu.RUnlock()
		if st, ok := cached.(*model.CanonicalState); ok {
			return st, sum, nil
		}
	} else {
		s.cacheMu.RUnlock()
	}

	rows, err := s.sql.QueryContext(ctx, `
		SELECT e.entity_kind, b.data
		FROM tm_snapshot_entities e
		JOIN tm_blobs b ON b.hash = e.blob_hash
		WHERE e.snapshot_id = ?
	`, sum.ID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	state := &model.CanonicalState{
		SnapshotID:  sum.ID,
		GeneratedAt: sum.CreatedAt,
	}
	for rows.Next() {
		var kind string
		var compressed []byte
		if err := rows.Scan(&kind, &compressed); err != nil {
			return nil, nil, err
		}
		raw, err := zstdDec.DecodeAll(compressed, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("decompress blob (%s): %w", kind, err)
		}
		if err := unmarshalEntityIntoState(state, kind, raw); err != nil {
			return nil, nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	sortCanonicalState(state)
	s.cacheSnapshot(sum.ID, state)
	return state, sum, nil
}

// ListSnapshots returns snapshot history summaries ordered newest first.
func (s *Store) ListSnapshots(ctx context.Context, limit, offset int) ([]model.SnapshotSummary, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM tm_snapshots`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.sql.QueryContext(ctx, `
		SELECT s.id, s.created_at, s.last_verified_at, s.trigger, s.trigger_detail, s.state_hash, s.summary_json,
		       (SELECT COUNT(*) FROM tm_events e WHERE e.snapshot_id = s.id)
		FROM tm_snapshots s
		ORDER BY s.id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []model.SnapshotSummary
	for rows.Next() {
		var sum model.SnapshotSummary
		var createdStr, verifiedStr, countsJSON string
		if err := rows.Scan(&sum.ID, &createdStr, &verifiedStr, &sum.Trigger, &sum.TriggerDetail, &sum.StateHash, &countsJSON, &sum.EventCount); err != nil {
			return nil, 0, err
		}
		sum.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		sum.LastVerifiedAt, _ = time.Parse(time.RFC3339Nano, verifiedStr)
		_ = json.Unmarshal([]byte(countsJSON), &sum.Counts)
		out = append(out, sum)
	}
	return out, total, rows.Err()
}

// ListEvents returns Time Machine timeline events filtered optionally by entity or snapshot range.
func (s *Store) ListEvents(ctx context.Context, entityKind, entityID string, limit int) ([]model.TimelineEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, snapshot_id, occurred_at, event_type, entity_kind, entity_id, entity_name, summary, diff_json FROM tm_events`
	var args []any
	var conds []string
	if entityKind != "" {
		conds = append(conds, `entity_kind = ?`)
		args = append(args, entityKind)
	}
	if entityID != "" {
		conds = append(conds, `entity_id = ?`)
		args = append(args, entityID)
	}
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.TimelineEvent
	for rows.Next() {
		var ev model.TimelineEvent
		var occ string
		if err := rows.Scan(&ev.ID, &ev.SnapshotID, &occ, &ev.EventType, &ev.EntityKind, &ev.EntityID, &ev.EntityName, &ev.Summary, &ev.DiffJSON); err != nil {
			return nil, err
		}
		ev.OccurredAt, _ = time.Parse(time.RFC3339Nano, occ)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// DiffSnapshots compares two snapshots by entity blob hashes and returns field-level diffs.
func (s *Store) DiffSnapshots(ctx context.Context, fromAt, toAt string) (*model.SnapshotDiff, error) {
	fromSum, err := s.ResolveSnapshot(ctx, fromAt)
	if err != nil {
		return nil, fmt.Errorf("resolve 'from' snapshot: %w", err)
	}
	toSum, err := s.ResolveSnapshot(ctx, toAt)
	if err != nil {
		return nil, fmt.Errorf("resolve 'to' snapshot: %w", err)
	}

	loadMap := func(snapID int64) (map[string]string, error) {
		rows, err := s.sql.QueryContext(ctx, `SELECT entity_kind, entity_id, blob_hash FROM tm_snapshot_entities WHERE snapshot_id = ?`, snapID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		m := make(map[string]string)
		for rows.Next() {
			var k, id, h string
			if err := rows.Scan(&k, &id, &h); err != nil {
				return nil, err
			}
			m[k+"\x00"+id] = h
		}
		return m, rows.Err()
	}

	fromMap, err := loadMap(fromSum.ID)
	if err != nil {
		return nil, err
	}
	toMap, err := loadMap(toSum.ID)
	if err != nil {
		return nil, err
	}

	diff := &model.SnapshotDiff{
		FromSnapshot: *fromSum,
		ToSnapshot:   *toSum,
	}

	allKeys := make(map[string]struct{}, len(fromMap)+len(toMap))
	for k := range fromMap {
		allKeys[k] = struct{}{}
	}
	for k := range toMap {
		allKeys[k] = struct{}{}
	}
	sortedKeys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	loadBlobObj := func(hash string) (map[string]any, string, error) {
		var compressed []byte
		if err := s.sql.QueryRowContext(ctx, `SELECT data FROM tm_blobs WHERE hash = ?`, hash).Scan(&compressed); err != nil {
			return nil, "", err
		}
		raw, err := zstdDec.DecodeAll(compressed, nil)
		if err != nil {
			return nil, "", err
		}
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, "", err
		}
		return obj, extractDisplayName(obj), nil
	}

	for _, key := range sortedKeys {
		parts := strings.SplitN(key, "\x00", 2)
		kind, id := parts[0], parts[1]
		if kind == kindIssue {
			// Surface entity diffs cleanly; issues have their own view, though include if helpful? Let's skip noise or keep structural entities.
			continue
		}
		hFrom, inFrom := fromMap[key]
		hTo, inTo := toMap[key]
		if inFrom && inTo && hFrom == hTo {
			continue
		}
		if !inFrom && inTo {
			obj, name, err := loadBlobObj(hTo)
			if err != nil {
				return nil, err
			}
			diff.AddedCount++
			diff.Entities = append(diff.Entities, model.EntityDiff{
				EntityKind: kind,
				EntityID:   id,
				Name:       name,
				Change:     "added",
				After:      obj,
			})
		} else if inFrom && !inTo {
			obj, name, err := loadBlobObj(hFrom)
			if err != nil {
				return nil, err
			}
			diff.RemovedCount++
			diff.Entities = append(diff.Entities, model.EntityDiff{
				EntityKind: kind,
				EntityID:   id,
				Name:       name,
				Change:     "removed",
				Before:     obj,
			})
		} else {
			beforeObj, nameBefore, err := loadBlobObj(hFrom)
			if err != nil {
				return nil, err
			}
			afterObj, nameAfter, err := loadBlobObj(hTo)
			if err != nil {
				return nil, err
			}
			name := nameAfter
			if name == "" {
				name = nameBefore
			}
			fields := computeFieldDiffs(beforeObj, afterObj)
			if len(fields) == 0 {
				continue
			}
			diff.ModifiedCount++
			diff.Entities = append(diff.Entities, model.EntityDiff{
				EntityKind: kind,
				EntityID:   id,
				Name:       name,
				Change:     "modified",
				Fields:     fields,
				Before:     beforeObj,
				After:      afterObj,
			})
		}
	}

	return diff, nil
}

// PruneRetention enforces snapshot retention and sweeps orphaned tm_blobs.
func (s *Store) PruneRetention(ctx context.Context, keepAllDays int) (int64, error) {
	if keepAllDays <= 0 {
		keepAllDays = 30
	}
	cutoff := time.Now().UTC().Add(-time.Duration(keepAllDays) * 24 * time.Hour).Format(time.RFC3339Nano)

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Always retain at least the latest snapshot, plus one snapshot per day older than cutoff.
	res, err := s.sql.ExecContext(ctx, `
		DELETE FROM tm_snapshots
		WHERE created_at < ?
		  AND id != (SELECT MAX(id) FROM tm_snapshots)
		  AND id NOT IN (
		      SELECT MAX(id) FROM tm_snapshots
		      WHERE created_at < ?
		      GROUP BY substr(created_at, 1, 10)
		  )
	`, cutoff, cutoff)
	if err != nil {
		return 0, err
	}
	deleted, _ := res.RowsAffected()

	if _, err := s.sql.ExecContext(ctx, `DELETE FROM tm_blobs WHERE hash NOT IN (SELECT DISTINCT blob_hash FROM tm_snapshot_entities)`); err != nil {
		return deleted, err
	}
	return deleted, nil
}

func (s *Store) cacheSnapshot(id int64, st *model.CanonicalState) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if len(s.snapshotCache) > 32 {
		s.snapshotCache = make(map[int64]any)
	}
	s.snapshotCache[id] = st
}

func flattenState(state *model.CanonicalState) ([]entityRecord, string, model.SnapshotCounts, error) {
	var records []entityRecord
	add := func(kind, id, name string, val any) error {
		raw, err := json.Marshal(val)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		records = append(records, entityRecord{
			kind:     kind,
			id:       id,
			name:     name,
			rawJSON:  raw,
			blobHash: hex.EncodeToString(sum[:]),
		})
		return nil
	}

	for _, x := range state.Sites {
		if err := add(kindSite, x.ID, x.Name, x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.Locations {
		if err := add(kindLocation, x.ID, x.Name, x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.OwnerGroups {
		if err := add(kindOwnerGroup, x.ID, x.Name, x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.Owners {
		if err := add(kindOwner, x.ID, x.Name, x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.Contacts {
		if err := add(kindContact, x.ID, x.Name, x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.DeviceTypes {
		if err := add(kindDeviceType, x.ID, x.Model, x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.ModuleTypes {
		if err := add(kindModuleType, x.ID, x.Model, x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.Racks {
		if err := add(kindRack, x.ID, x.Name, x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	autoCount := 0
	for _, x := range state.Devices {
		if x.Origin == "auto" {
			autoCount++
		}
		// Exclude volatile LastSeenAt from snapshot hash calculation so idle heartbeat/polls
		// that observe identical topology don't create noisy snapshots.
		copyDev := x
		copyDev.LastSeenAt = time.Time{}
		copyDev.FirstSeenAt = time.Time{}
		for i := range copyDev.Provenance {
			copyDev.Provenance[i].ObservedAt = time.Time{}
		}
		if err := add(kindDevice, x.ID, x.Name, copyDev); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.VLANs {
		if err := add(kindVLAN, x.ID, fmt.Sprintf("VLAN %d (%s)", x.VID, x.Name), x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.Prefixes {
		if err := add(kindPrefix, x.ID, x.CIDR, x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.Cables {
		if err := add(kindCable, x.ID, fmt.Sprintf("%s:%s <-> %s:%s", x.ADevice, x.APort, x.BDevice, x.BPort), x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.Assets {
		if err := add(kindAsset, x.ID, x.Name, x); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}
	for _, x := range state.Issues {
		copyIss := x
		copyIss.DetectedAt = time.Time{}
		if err := add(kindIssue, x.ID, x.Message, copyIss); err != nil {
			return nil, "", model.SnapshotCounts{}, err
		}
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].kind != records[j].kind {
			return records[i].kind < records[j].kind
		}
		return records[i].id < records[j].id
	})

	h := sha256.New()
	for _, r := range records {
		h.Write([]byte(r.kind))
		h.Write([]byte{0})
		h.Write([]byte(r.id))
		h.Write([]byte{0})
		h.Write([]byte(r.blobHash))
		h.Write([]byte{'\n'})
	}
	stateHash := hex.EncodeToString(h.Sum(nil))

	counts := model.SnapshotCounts{
		Sites:       len(state.Sites),
		Racks:       len(state.Racks),
		Devices:     len(state.Devices),
		AutoDevices: autoCount,
		Cables:      len(state.Cables),
		VLANs:       len(state.VLANs),
		Prefixes:    len(state.Prefixes),
		Assets:      len(state.Assets),
		Owners:      len(state.Owners),
		Issues:      len(state.Issues),
	}
	return records, stateHash, counts, nil
}

func buildTimelineEvents(
	ctx context.Context,
	tx *sql.Tx,
	snapID int64,
	nowStr, trigger, detail string,
	prevID int64,
	prevEntities map[string]string,
	currMap map[string]entityRecord,
) []model.TimelineEvent {
	var events []model.TimelineEvent
	if prevID == 0 {
		events = append(events, model.TimelineEvent{
			SnapshotID: snapID,
			EventType:  "snapshot.initial",
			EntityKind: "system",
			EntityID:   "system",
			EntityName: "Auto Netviz",
			Summary:    fmt.Sprintf("Initial inventory snapshot (%s)", trigger),
			DiffJSON:   "[]",
		})
		return events
	}

	loadPrevObj := func(hash string) map[string]any {
		var compressed []byte
		if err := tx.QueryRowContext(ctx, `SELECT data FROM tm_blobs WHERE hash = ?`, hash).Scan(&compressed); err != nil {
			return nil
		}
		raw, err := zstdDec.DecodeAll(compressed, nil)
		if err != nil {
			return nil
		}
		var obj map[string]any
		_ = json.Unmarshal(raw, &obj)
		return obj
	}

	if trigger == "manual_reload" {
		events = append(events, model.TimelineEvent{
			SnapshotID: snapID,
			EventType:  "manual.reloaded",
			EntityKind: "system",
			EntityID:   "manual",
			EntityName: "Manual YAML",
			Summary:    "Manual YAML files reloaded: " + detail,
			DiffJSON:   "[]",
		})
	}

	for key, rec := range currMap {
		if rec.kind == kindIssue {
			continue
		}
		prevHash, existed := prevEntities[key]
		if !existed {
			evType := rec.kind + ".added"
			if rec.kind == kindCable {
				evType = "cable.connected"
			}
			events = append(events, model.TimelineEvent{
				SnapshotID: snapID,
				EventType:  evType,
				EntityKind: rec.kind,
				EntityID:   rec.id,
				EntityName: rec.name,
				Summary:    fmt.Sprintf("%s %q added", rec.kind, rec.name),
				DiffJSON:   "[]",
			})
			continue
		}
		if prevHash == rec.blobHash {
			continue
		}
		beforeObj := loadPrevObj(prevHash)
		var afterObj map[string]any
		_ = json.Unmarshal(rec.rawJSON, &afterObj)
		diffs := computeFieldDiffs(beforeObj, afterObj)
		if len(diffs) == 0 {
			continue
		}
		diffBytes, _ := json.Marshal(diffs)
		evType, summary := classifyEntityChange(rec.kind, rec.name, diffs)
		events = append(events, model.TimelineEvent{
			SnapshotID: snapID,
			EventType:  evType,
			EntityKind: rec.kind,
			EntityID:   rec.id,
			EntityName: rec.name,
			Summary:    summary,
			DiffJSON:   string(diffBytes),
		})
	}

	for key, prevHash := range prevEntities {
		parts := strings.SplitN(key, "\x00", 2)
		kind, id := parts[0], parts[1]
		if kind == kindIssue {
			continue
		}
		if _, stillThere := currMap[key]; stillThere {
			continue
		}
		obj := loadPrevObj(prevHash)
		name := extractDisplayName(obj)
		if name == "" {
			name = id
		}
		evType := kind + ".removed"
		if kind == kindCable {
			evType = "cable.disconnected"
		}
		events = append(events, model.TimelineEvent{
			SnapshotID: snapID,
			EventType:  evType,
			EntityKind: kind,
			EntityID:   id,
			EntityName: name,
			Summary:    fmt.Sprintf("%s %q removed", kind, name),
			DiffJSON:   "[]",
		})
	}
	return events
}

func classifyEntityChange(kind, name string, diffs []model.FieldDiff) (string, string) {
	changed := make([]string, 0, len(diffs))
	for _, d := range diffs {
		changed = append(changed, d.Field)
		if kind == kindDevice {
			switch d.Field {
			case "status":
				return "device.status_changed", fmt.Sprintf("Device %q status %v -> %v", name, d.Old, d.New)
			case "rack", "position", "face":
				return "rack.moved", fmt.Sprintf("Device %q rack placement changed", name)
			case "owner":
				return "owner.changed", fmt.Sprintf("Device %q owner %v -> %v", name, d.Old, d.New)
			case "modules", "module_bays":
				return "module.changed", fmt.Sprintf("Device %q hardware modules updated", name)
			case "interfaces":
				return "interface.changed", fmt.Sprintf("Device %q interfaces/VLANs updated", name)
			}
		}
	}
	return kind + ".updated", fmt.Sprintf("%s %q updated (%s)", kind, name, strings.Join(changed, ", "))
}

func computeFieldDiffs(before, after map[string]any) []model.FieldDiff {
	keys := make(map[string]struct{})
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		if k == "last_seen_at" || k == "first_seen_at" {
			continue
		}
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var diffs []model.FieldDiff
	for _, k := range sorted {
		vOld, okOld := before[k]
		vNew, okNew := after[k]
		if okOld == okNew && reflect.DeepEqual(vOld, vNew) {
			continue
		}
		diffs = append(diffs, model.FieldDiff{Field: k, Old: vOld, New: vNew})
	}
	return diffs
}

func extractDisplayName(obj map[string]any) string {
	for _, key := range []string{"name", "model", "cidr", "label", "id"} {
		if v, ok := obj[key].(string); ok && v != "" {
			return v
		}
	}
	if aDev, ok := obj["a_device"].(string); ok {
		bDev, _ := obj["b_device"].(string)
		aPort, _ := obj["a_port"].(string)
		bPort, _ := obj["b_port"].(string)
		return fmt.Sprintf("%s:%s <-> %s:%s", aDev, aPort, bDev, bPort)
	}
	return ""
}

func unmarshalEntityIntoState(state *model.CanonicalState, kind string, raw []byte) error {
	switch kind {
	case kindSite:
		var x model.Site
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.Sites = append(state.Sites, x)
	case kindLocation:
		var x model.Location
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.Locations = append(state.Locations, x)
	case kindOwnerGroup:
		var x model.OwnerGroup
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.OwnerGroups = append(state.OwnerGroups, x)
	case kindOwner:
		var x model.Owner
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.Owners = append(state.Owners, x)
	case kindContact:
		var x model.Contact
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.Contacts = append(state.Contacts, x)
	case kindDeviceType:
		var x model.DeviceType
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.DeviceTypes = append(state.DeviceTypes, x)
	case kindModuleType:
		var x model.ModuleType
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.ModuleTypes = append(state.ModuleTypes, x)
	case kindRack:
		var x model.Rack
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.Racks = append(state.Racks, x)
	case kindDevice:
		var x model.Device
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.Devices = append(state.Devices, x)
	case kindVLAN:
		var x model.VLAN
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.VLANs = append(state.VLANs, x)
	case kindPrefix:
		var x model.Prefix
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.Prefixes = append(state.Prefixes, x)
	case kindCable:
		var x model.Cable
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.Cables = append(state.Cables, x)
	case kindAsset:
		var x model.Asset
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.Assets = append(state.Assets, x)
	case kindIssue:
		var x model.Issue
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		state.Issues = append(state.Issues, x)
	}
	return nil
}

func sortCanonicalState(state *model.CanonicalState) {
	sort.Slice(state.Sites, func(i, j int) bool { return state.Sites[i].ID < state.Sites[j].ID })
	sort.Slice(state.Locations, func(i, j int) bool { return state.Locations[i].ID < state.Locations[j].ID })
	sort.Slice(state.OwnerGroups, func(i, j int) bool { return state.OwnerGroups[i].Name < state.OwnerGroups[j].Name })
	sort.Slice(state.Owners, func(i, j int) bool { return state.Owners[i].Name < state.Owners[j].Name })
	sort.Slice(state.Contacts, func(i, j int) bool { return state.Contacts[i].Name < state.Contacts[j].Name })
	sort.Slice(state.DeviceTypes, func(i, j int) bool { return state.DeviceTypes[i].ID < state.DeviceTypes[j].ID })
	sort.Slice(state.ModuleTypes, func(i, j int) bool { return state.ModuleTypes[i].ID < state.ModuleTypes[j].ID })
	sort.Slice(state.Racks, func(i, j int) bool { return state.Racks[i].ID < state.Racks[j].ID })
	sort.Slice(state.Devices, func(i, j int) bool { return state.Devices[i].Name < state.Devices[j].Name })
	sort.Slice(state.VLANs, func(i, j int) bool {
		if state.VLANs[i].Site != state.VLANs[j].Site {
			return state.VLANs[i].Site < state.VLANs[j].Site
		}
		return state.VLANs[i].VID < state.VLANs[j].VID
	})
	sort.Slice(state.Prefixes, func(i, j int) bool { return state.Prefixes[i].CIDR < state.Prefixes[j].CIDR })
	sort.Slice(state.Cables, func(i, j int) bool { return state.Cables[i].ID < state.Cables[j].ID })
	sort.Slice(state.Assets, func(i, j int) bool { return state.Assets[i].Name < state.Assets[j].Name })
}
