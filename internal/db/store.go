package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blalange/auto-netviz/internal/model"
	"github.com/blalange/auto-netviz/internal/protocol"
)

// StoredObservation holds one active collector observation row from SQLite.
type StoredObservation struct {
	ID          int64
	CollectorID string
	Site        string
	Source      string
	Profile     string
	Kind        string // device | link | vlan | prefix
	ObsKey      string
	PayloadJSON string
	ObservedAt  time.Time
	ExpiresAt   time.Time
}

// IdentityRecord persists canonical device identities so auto-created devices
// preserve stable IDs across restarts and promotion to manual YAML.
type IdentityRecord struct {
	CanonicalID string
	EntityKind  string
	Origin      string
	ManualKey   string
	StrongKeys  []string
	DisplayName string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

type SearchResult struct {
	EntityKind string `json:"entity_kind"`
	EntityID   string `json:"entity_id"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle"`
}

func (s *Store) UpsertCollectorHello(ctx context.Context, hello protocol.HelloPayload) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_, err := s.sql.ExecContext(ctx, `
		INSERT INTO collectors (collector_id, name, site, zone, version, hostname, status, connected_at, last_seen_at, uptime_seconds, queue_depth)
		VALUES (?, ?, ?, ?, ?, ?, 'online', ?, ?, 0, 0)
		ON CONFLICT(collector_id) DO UPDATE SET
			name = excluded.name,
			site = excluded.site,
			zone = excluded.zone,
			version = excluded.version,
			hostname = excluded.hostname,
			status = 'online',
			connected_at = excluded.connected_at,
			last_seen_at = excluded.last_seen_at
	`, hello.CollectorID, hello.Name, hello.Site, hello.Zone, hello.Version, hello.Hostname, now, now)
	return err
}

func (s *Store) UpdateCollectorHeartbeat(ctx context.Context, hb protocol.HeartbeatPayload) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		UPDATE collectors SET status = 'online', last_seen_at = ?, uptime_seconds = ?, queue_depth = ?
		WHERE collector_id = ?
	`, now, hb.UptimeSeconds, hb.QueueDepth, hb.CollectorID)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO collector_source_status (collector_id, source, profile, enabled, status, last_run_at, last_success_at, duration_ms, device_count, link_count, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(collector_id, source, profile) DO UPDATE SET
			enabled = excluded.enabled,
			status = excluded.status,
			last_run_at = excluded.last_run_at,
			last_success_at = excluded.last_success_at,
			duration_ms = excluded.duration_ms,
			device_count = excluded.device_count,
			link_count = excluded.link_count,
			error = excluded.error
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, src := range hb.Sources {
		en := 0
		if src.Enabled {
			en = 1
		}
		var lastRun, lastSucc sql.NullString
		if src.LastRunAt != nil {
			lastRun = sql.NullString{String: src.LastRunAt.UTC().Format(time.RFC3339Nano), Valid: true}
		}
		if src.LastSuccessAt != nil {
			lastSucc = sql.NullString{String: src.LastSuccessAt.UTC().Format(time.RFC3339Nano), Valid: true}
		}
		if _, err := stmt.ExecContext(ctx, hb.CollectorID, src.Source, src.Profile, en, src.Status, lastRun, lastSucc, src.DurationMs, src.DeviceCount, src.LinkCount, src.Error); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) MarkCollectorOffline(ctx context.Context, collectorID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.sql.ExecContext(ctx, `UPDATE collectors SET status = 'offline' WHERE collector_id = ?`, collectorID)
	return err
}

func (s *Store) ListCollectors(ctx context.Context) ([]model.CollectorInfo, error) {
	rows, err := s.sql.QueryContext(ctx, `
		SELECT collector_id, name, site, zone, version, hostname, status, connected_at, last_seen_at, uptime_seconds, queue_depth
		FROM collectors ORDER BY collector_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.CollectorInfo
	byID := make(map[string]int)
	for rows.Next() {
		var c model.CollectorInfo
		var connAt sql.NullString
		var lastSeen string
		if err := rows.Scan(&c.CollectorID, &c.Name, &c.Site, &c.Zone, &c.Version, &c.Hostname, &c.Status, &connAt, &lastSeen, &c.UptimeSeconds, &c.QueueDepth); err != nil {
			return nil, err
		}
		if connAt.Valid {
			if t, err := time.Parse(time.RFC3339Nano, connAt.String); err == nil {
				c.ConnectedAt = &t
			}
		}
		c.LastSeenAt, _ = time.Parse(time.RFC3339Nano, lastSeen)
		byID[c.CollectorID] = len(out)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sRows, err := s.sql.QueryContext(ctx, `
		SELECT collector_id, source, profile, enabled, status, last_run_at, last_success_at, duration_ms, device_count, link_count, error
		FROM collector_source_status ORDER BY collector_id, source, profile
	`)
	if err != nil {
		return nil, err
	}
	defer sRows.Close()

	for sRows.Next() {
		var cid string
		var src model.CollectorSourceInfo
		var en int
		var lr, ls sql.NullString
		if err := sRows.Scan(&cid, &src.Source, &src.Profile, &en, &src.Status, &lr, &ls, &src.DurationMs, &src.DeviceCount, &src.LinkCount, &src.Error); err != nil {
			return nil, err
		}
		src.Enabled = en != 0
		if lr.Valid {
			if t, err := time.Parse(time.RFC3339Nano, lr.String); err == nil {
				src.LastRunAt = &t
			}
		}
		if ls.Valid {
			if t, err := time.Parse(time.RFC3339Nano, ls.String); err == nil {
				src.LastSuccessAt = &t
			}
		}
		if idx, ok := byID[cid]; ok {
			out[idx].Sources = append(out[idx].Sources, src)
		}
	}
	return out, sRows.Err()
}

// SaveObservationBatch stores incoming collector observations into raw_observations.
// When batch.FullSnapshot is true, previously active items from (collector, source, profile)
// absent from this batch are deactivated immediately.
func (s *Store) SaveObservationBatch(ctx context.Context, batch protocol.ObservationBatch) error {
	ttl := batch.TTLSeconds
	if ttl <= 0 {
		ttl = 600
	}
	collectedAt := batch.CollectedAt
	if collectedAt.IsZero() {
		collectedAt = time.Now().UTC()
	}
	obsAtStr := collectedAt.UTC().Format(time.RFC3339Nano)
	expAtStr := collectedAt.UTC().Add(time.Duration(ttl) * time.Second).Format(time.RFC3339Nano)
	profile := batch.Profile
	if profile == "" {
		profile = "default"
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if batch.FullSnapshot {
		if _, err := tx.ExecContext(ctx,
			`UPDATE raw_observations SET is_active = 0 WHERE collector_id = ? AND source = ? AND profile = ?`,
			batch.CollectorID, batch.Source, profile,
		); err != nil {
			return err
		}
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO raw_observations (collector_id, site, source, profile, kind, obs_key, payload_json, observed_at, expires_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(collector_id, source, profile, kind, obs_key) DO UPDATE SET
			site = excluded.site,
			payload_json = excluded.payload_json,
			observed_at = excluded.observed_at,
			expires_at = excluded.expires_at,
			is_active = 1
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	save := func(kind, key string, v any) error {
		if key == "" {
			return nil
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = stmt.ExecContext(ctx, batch.CollectorID, batch.Site, batch.Source, profile, kind, key, string(raw), obsAtStr, expAtStr)
		return err
	}

	for _, d := range batch.Devices {
		key := d.ObsKey
		if key == "" {
			key = d.Name
		}
		if err := save("device", key, d); err != nil {
			return err
		}
	}
	for _, l := range batch.Links {
		key := l.ObsKey
		if key == "" {
			key = fmt.Sprintf("%s:%s->%s:%s", l.LocalDeviceName, l.LocalPort, l.RemoteDeviceName, l.RemotePort)
		}
		if err := save("link", key, l); err != nil {
			return err
		}
	}
	for _, v := range batch.VLANs {
		key := fmt.Sprintf("%s:%d", v.Site, v.VID)
		if err := save("vlan", key, v); err != nil {
			return err
		}
	}
	for _, p := range batch.Prefixes {
		if err := save("prefix", p.CIDR, p); err != nil {
			return err
		}
	}

	_, _ = tx.ExecContext(ctx, `UPDATE collectors SET last_seen_at = ? WHERE collector_id = ?`, time.Now().UTC().Format(time.RFC3339Nano), batch.CollectorID)

	return tx.Commit()
}

// LoadActiveObservations expires stale rows past TTL and returns all active observations.
func (s *Store) LoadActiveObservations(ctx context.Context) ([]StoredObservation, error) {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)

	s.writeMu.Lock()
	_, _ = s.sql.ExecContext(ctx, `UPDATE raw_observations SET is_active = 0 WHERE is_active = 1 AND expires_at < ?`, nowStr)
	s.writeMu.Unlock()

	rows, err := s.sql.QueryContext(ctx, `
		SELECT id, collector_id, site, source, profile, kind, obs_key, payload_json, observed_at, expires_at
		FROM raw_observations
		WHERE is_active = 1
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StoredObservation
	for rows.Next() {
		var o StoredObservation
		var obsStr, expStr string
		if err := rows.Scan(&o.ID, &o.CollectorID, &o.Site, &o.Source, &o.Profile, &o.Kind, &o.ObsKey, &o.PayloadJSON, &obsStr, &expStr); err != nil {
			return nil, err
		}
		o.ObservedAt, _ = time.Parse(time.RFC3339Nano, obsStr)
		o.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expStr)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) LoadIdentities(ctx context.Context) ([]IdentityRecord, error) {
	rows, err := s.sql.QueryContext(ctx, `
		SELECT canonical_id, entity_kind, origin, manual_key, strong_keys_json, display_name, first_seen_at, last_seen_at
		FROM entity_identities
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IdentityRecord
	for rows.Next() {
		var r IdentityRecord
		var keysJSON, firstStr, lastStr string
		if err := rows.Scan(&r.CanonicalID, &r.EntityKind, &r.Origin, &r.ManualKey, &keysJSON, &r.DisplayName, &firstStr, &lastStr); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(keysJSON), &r.StrongKeys)
		r.FirstSeenAt, _ = time.Parse(time.RFC3339Nano, firstStr)
		r.LastSeenAt, _ = time.Parse(time.RFC3339Nano, lastStr)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpsertIdentities(ctx context.Context, records []IdentityRecord) error {
	if len(records) == 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO entity_identities (canonical_id, entity_kind, origin, manual_key, strong_keys_json, display_name, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(canonical_id) DO UPDATE SET
			origin = excluded.origin,
			manual_key = excluded.manual_key,
			strong_keys_json = excluded.strong_keys_json,
			display_name = excluded.display_name,
			last_seen_at = excluded.last_seen_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range records {
		keysRaw, _ := json.Marshal(r.StrongKeys)
		if _, err := stmt.ExecContext(ctx,
			r.CanonicalID, r.EntityKind, r.Origin, r.ManualKey, string(keysRaw), r.DisplayName,
			r.FirstSeenAt.UTC().Format(time.RFC3339Nano), r.LastSeenAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RecordManualRevision(ctx context.Context, hash string, filesCount, issuesCount int) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.sql.ExecContext(ctx,
		`INSERT INTO manual_revisions (hash, files_count, issues_count, loaded_at) VALUES (?, ?, ?, ?)`,
		hash, filesCount, issuesCount, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

// MaterializeState writes the latest merged canonical state into the normalized
// current tables and rebuilds the SQLite FTS5 search index.
func (s *Store) MaterializeState(ctx context.Context, state *model.CanonicalState) error {
	if state == nil {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	clearTables := []string{
		"search_fts", "issues", "assets", "cables", "prefixes", "vlans",
		"device_modules", "device_module_bays", "ip_addresses", "interfaces",
		"devices", "racks", "module_types", "device_types", "contacts",
		"owners", "owner_groups", "locations", "sites",
	}
	for _, tbl := range clearTables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+tbl); err != nil {
			return err
		}
	}

	ftsStmt, err := tx.PrepareContext(ctx, `INSERT INTO search_fts (entity_kind, entity_id, title, subtitle, body) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ftsStmt.Close()

	mustJSON := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}

	for _, x := range state.Sites {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sites (id, name, status, facility, address, description, payload_json) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			x.ID, x.Name, x.Status, x.Facility, x.Address, x.Description, mustJSON(x)); err != nil {
			return err
		}
		_, _ = ftsStmt.ExecContext(ctx, "site", x.ID, x.Name, x.Facility, x.Description+" "+x.Address)
	}
	for _, x := range state.Locations {
		if _, err := tx.ExecContext(ctx, `INSERT INTO locations (id, site, name, payload_json) VALUES (?, ?, ?, ?)`, x.ID, x.Site, x.Name, mustJSON(x)); err != nil {
			return err
		}
	}
	for _, x := range state.OwnerGroups {
		if _, err := tx.ExecContext(ctx, `INSERT INTO owner_groups (id, name, payload_json) VALUES (?, ?, ?)`, x.ID, x.Name, mustJSON(x)); err != nil {
			return err
		}
	}
	for _, x := range state.Owners {
		if _, err := tx.ExecContext(ctx, `INSERT INTO owners (id, name, owner_group, payload_json) VALUES (?, ?, ?, ?)`, x.ID, x.Name, x.Group, mustJSON(x)); err != nil {
			return err
		}
		_, _ = ftsStmt.ExecContext(ctx, "owner", x.ID, x.Name, x.Group, x.Email+" "+x.Description)
	}
	for _, x := range state.Contacts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO contacts (id, name, role, owner, payload_json) VALUES (?, ?, ?, ?, ?)`, x.ID, x.Name, x.Role, x.Owner, mustJSON(x)); err != nil {
			return err
		}
		_, _ = ftsStmt.ExecContext(ctx, "contact", x.ID, x.Name, x.Role, x.Owner+" "+x.Email)
	}
	for _, x := range state.DeviceTypes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_types (id, manufacturer, model, u_height, payload_json) VALUES (?, ?, ?, ?, ?)`, x.ID, x.Manufacturer, x.Model, x.UHeight, mustJSON(x)); err != nil {
			return err
		}
	}
	for _, x := range state.ModuleTypes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO module_types (id, manufacturer, model, profile, payload_json) VALUES (?, ?, ?, ?, ?)`, x.ID, x.Manufacturer, x.Model, x.Profile, mustJSON(x)); err != nil {
			return err
		}
	}
	for _, x := range state.Racks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO racks (id, name, site, location, u_height, used_u, payload_json) VALUES (?, ?, ?, ?, ?, ?, ?)`, x.ID, x.Name, x.Site, x.Location, x.UHeight, x.UsedU, mustJSON(x)); err != nil {
			return err
		}
		_, _ = ftsStmt.ExecContext(ctx, "rack", x.ID, x.Name, x.Site, fmt.Sprintf("%dU %s %s", x.UHeight, x.Location, x.Description))
	}

	for _, d := range state.Devices {
		if _, err := tx.ExecContext(ctx, `INSERT INTO devices (id, name, origin, site, role, device_type, status, rack_id, owner, primary_ip, oob_ip, serial, asset_tag, payload_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, d.Name, d.Origin, d.Site, d.Role, d.DeviceType, d.Status, d.RackID, d.Owner, d.PrimaryIP, d.OOBIP, d.Serial, d.AssetTag, mustJSON(d)); err != nil {
			return err
		}
		var bodyParts []string
		bodyParts = append(bodyParts, d.Manufacturer, d.Model, d.Serial, d.ServiceTag, d.AssetTag, d.PrimaryIP, d.OOBIP, d.Owner, d.Rack, d.Description)
		for _, iface := range d.Interfaces {
			if _, err := tx.ExecContext(ctx, `INSERT INTO interfaces (id, device_id, name, mac_address, untagged_vlan, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
				iface.ID, d.ID, iface.Name, iface.MACAddress, iface.UntaggedVLAN, mustJSON(iface)); err != nil {
				return err
			}
			bodyParts = append(bodyParts, iface.Name, iface.MACAddress)
			for _, ip := range iface.IPAddresses {
				if _, err := tx.ExecContext(ctx, `INSERT INTO ip_addresses (id, address, ip, prefix, device_id, interface, payload_json) VALUES (?, ?, ?, ?, ?, ?, ?)`,
					ip.ID, ip.Address, ip.IP, ip.Prefix, d.ID, iface.Name, mustJSON(ip)); err != nil {
					return err
				}
				bodyParts = append(bodyParts, ip.Address, ip.DNSName)
			}
		}
		for _, mb := range d.ModuleBays {
			if _, err := tx.ExecContext(ctx, `INSERT INTO device_module_bays (id, device_id, name, payload_json) VALUES (?, ?, ?, ?)`,
				d.ID+":"+mb.Name, d.ID, mb.Name, mustJSON(mb)); err != nil {
				return err
			}
		}
		for _, mod := range d.Modules {
			if _, err := tx.ExecContext(ctx, `INSERT INTO device_modules (id, device_id, bay, module_type, serial, asset_tag, payload_json) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				mod.ID, d.ID, mod.Bay, mod.ModuleType, mod.Serial, mod.AssetTag, mustJSON(mod)); err != nil {
				return err
			}
			bodyParts = append(bodyParts, mod.Model, mod.Serial)
		}
		_, _ = ftsStmt.ExecContext(ctx, "device", d.ID, d.Name, fmt.Sprintf("%s · %s · %s", d.Role, d.Site, d.PrimaryIP), strings.Join(bodyParts, " "))
	}

	for _, x := range state.VLANs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO vlans (id, site, vid, name, payload_json) VALUES (?, ?, ?, ?, ?)`, x.ID, x.Site, x.VID, x.Name, mustJSON(x)); err != nil {
			return err
		}
		_, _ = ftsStmt.ExecContext(ctx, "vlan", x.ID, fmt.Sprintf("VLAN %d — %s", x.VID, x.Name), x.Site, x.Description)
	}
	for _, x := range state.Prefixes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO prefixes (id, cidr, site, vlan, payload_json) VALUES (?, ?, ?, ?, ?)`, x.ID, x.CIDR, x.Site, x.VLAN, mustJSON(x)); err != nil {
			return err
		}
		_, _ = ftsStmt.ExecContext(ctx, "prefix", x.ID, x.CIDR, x.Site, x.Description)
	}
	for _, x := range state.Cables {
		if _, err := tx.ExecContext(ctx, `INSERT INTO cables (id, a_device_id, a_port, b_device_id, b_port, method, confidence, payload_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			x.ID, x.ADeviceID, x.APort, x.BDeviceID, x.BPort, x.Method, x.Confidence, mustJSON(x)); err != nil {
			return err
		}
		_, _ = ftsStmt.ExecContext(ctx, "cable", x.ID, fmt.Sprintf("%s:%s ↔ %s:%s", x.ADevice, x.APort, x.BDevice, x.BPort), x.Type, x.Label+" "+x.Method)
	}
	for _, x := range state.Assets {
		if _, err := tx.ExecContext(ctx, `INSERT INTO assets (id, asset_tag, name, kind, category, serial, status, site, owner, payload_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			x.ID, x.AssetTag, x.Name, x.Kind, x.Category, x.Serial, x.Status, x.Site, x.Owner, mustJSON(x)); err != nil {
			return err
		}
		_, _ = ftsStmt.ExecContext(ctx, "asset", x.ID, x.Name, x.AssetTag+" · "+x.Kind, x.Manufacturer+" "+x.Model+" "+x.Serial+" "+x.Owner)
	}
	for _, x := range state.Issues {
		if _, err := tx.ExecContext(ctx, `INSERT INTO issues (id, severity, kind, entity_kind, entity_id, message, payload_json) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			x.ID, x.Severity, x.Kind, x.EntityKind, x.EntityID, x.Message, mustJSON(x)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Search performs FTS5 full-text search across current materialized state,
// falling back to substring filtering on historical states when atParam is set.
func (s *Store) Search(ctx context.Context, query string, atParam string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	if atParam != "" && !strings.EqualFold(atParam, "live") {
		state, _, err := s.LoadStateAt(ctx, atParam)
		if err != nil {
			return nil, err
		}
		return searchInMemoryState(state, query, limit), nil
	}

	// Build safe prefix FTS5 query tokens.
	tokens := strings.Fields(query)
	for i, tok := range tokens {
		cleaned := strings.NewReplacer(`"`, "", `'`, "", `*`, "", `:`, " ").Replace(tok)
		tokens[i] = `"` + cleaned + `"*`
	}
	ftsQuery := strings.Join(tokens, " ")

	rows, err := s.sql.QueryContext(ctx, `
		SELECT entity_kind, entity_id, title, subtitle
		FROM search_fts
		WHERE search_fts MATCH ?
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		// Fallback to LIKE if syntax is exotic.
		like := "%" + query + "%"
		rows, err = s.sql.QueryContext(ctx, `
			SELECT entity_kind, entity_id, title, subtitle
			FROM search_fts
			WHERE title LIKE ? OR subtitle LIKE ? OR body LIKE ?
			LIMIT ?
		`, like, like, like, limit)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.EntityKind, &r.EntityID, &r.Title, &r.Subtitle); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func searchInMemoryState(state *model.CanonicalState, query string, limit int) []SearchResult {
	q := strings.ToLower(query)
	var out []SearchResult
	addIfMatch := func(kind, id, title, subtitle string, haystack ...string) {
		if len(out) >= limit {
			return
		}
		for _, h := range append([]string{title, subtitle}, haystack...) {
			if strings.Contains(strings.ToLower(h), q) {
				out = append(out, SearchResult{EntityKind: kind, EntityID: id, Title: title, Subtitle: subtitle})
				return
			}
		}
	}
	for _, d := range state.Devices {
		addIfMatch("device", d.ID, d.Name, d.Role+" · "+d.PrimaryIP, d.Serial, d.AssetTag, d.OOBIP, d.Owner)
	}
	for _, r := range state.Racks {
		addIfMatch("rack", r.ID, r.Name, r.Site, r.Location)
	}
	for _, v := range state.VLANs {
		addIfMatch("vlan", v.ID, fmt.Sprintf("VLAN %d — %s", v.VID, v.Name), v.Site)
	}
	for _, p := range state.Prefixes {
		addIfMatch("prefix", p.ID, p.CIDR, p.Site, p.Description)
	}
	for _, a := range state.Assets {
		addIfMatch("asset", a.ID, a.Name, a.AssetTag, a.Serial, a.Owner)
	}
	for _, o := range state.Owners {
		addIfMatch("owner", o.ID, o.Name, o.Group, o.Email)
	}
	return out
}
