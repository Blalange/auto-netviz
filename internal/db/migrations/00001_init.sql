-- +goose Up
CREATE TABLE collectors (
    collector_id   TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    site           TEXT NOT NULL,
    zone           TEXT NOT NULL DEFAULT '',
    version        TEXT NOT NULL DEFAULT '',
    hostname       TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'offline',
    connected_at   TEXT,
    last_seen_at   TEXT NOT NULL,
    uptime_seconds INTEGER NOT NULL DEFAULT 0,
    queue_depth    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE collector_source_status (
    collector_id    TEXT NOT NULL,
    source          TEXT NOT NULL,
    profile         TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL,
    last_run_at     TEXT,
    last_success_at TEXT,
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    device_count    INTEGER NOT NULL DEFAULT 0,
    link_count      INTEGER NOT NULL DEFAULT 0,
    error           TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (collector_id, source, profile),
    FOREIGN KEY (collector_id) REFERENCES collectors(collector_id) ON DELETE CASCADE
);

CREATE TABLE raw_observations (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    collector_id TEXT NOT NULL,
    site         TEXT NOT NULL,
    source       TEXT NOT NULL,
    profile      TEXT NOT NULL,
    kind         TEXT NOT NULL,
    obs_key      TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    observed_at  TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    is_active    INTEGER NOT NULL DEFAULT 1,
    UNIQUE(collector_id, source, profile, kind, obs_key)
);

CREATE INDEX idx_raw_obs_active ON raw_observations(collector_id, source, profile, is_active);
CREATE INDEX idx_raw_obs_expires ON raw_observations(expires_at, is_active);

CREATE TABLE manual_revisions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    hash         TEXT NOT NULL,
    files_count  INTEGER NOT NULL,
    issues_count INTEGER NOT NULL,
    loaded_at    TEXT NOT NULL
);

CREATE TABLE entity_identities (
    canonical_id    TEXT PRIMARY KEY,
    entity_kind     TEXT NOT NULL,
    origin          TEXT NOT NULL,
    manual_key      TEXT NOT NULL DEFAULT '',
    strong_keys_json TEXT NOT NULL DEFAULT '[]',
    display_name    TEXT NOT NULL DEFAULT '',
    first_seen_at   TEXT NOT NULL,
    last_seen_at    TEXT NOT NULL,
    archived_at     TEXT
);

CREATE TABLE sites (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    status       TEXT NOT NULL,
    facility     TEXT NOT NULL DEFAULT '',
    address      TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL
);

CREATE TABLE locations (
    id           TEXT PRIMARY KEY,
    site         TEXT NOT NULL,
    name         TEXT NOT NULL,
    payload_json TEXT NOT NULL
);

CREATE TABLE owner_groups (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    payload_json TEXT NOT NULL
);

CREATE TABLE owners (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    owner_group  TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL
);

CREATE TABLE contacts (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    role         TEXT NOT NULL DEFAULT '',
    owner        TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL
);

CREATE TABLE device_types (
    id           TEXT PRIMARY KEY,
    manufacturer TEXT NOT NULL,
    model        TEXT NOT NULL,
    u_height     INTEGER NOT NULL DEFAULT 1,
    payload_json TEXT NOT NULL
);

CREATE TABLE module_types (
    id           TEXT PRIMARY KEY,
    manufacturer TEXT NOT NULL,
    model        TEXT NOT NULL,
    profile      TEXT NOT NULL,
    payload_json TEXT NOT NULL
);

CREATE TABLE racks (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    site         TEXT NOT NULL,
    location     TEXT NOT NULL DEFAULT '',
    u_height     INTEGER NOT NULL DEFAULT 42,
    used_u       INTEGER NOT NULL DEFAULT 0,
    payload_json TEXT NOT NULL
);

CREATE TABLE devices (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    origin       TEXT NOT NULL,
    site         TEXT NOT NULL,
    role         TEXT NOT NULL,
    device_type  TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL,
    rack_id      TEXT NOT NULL DEFAULT '',
    owner        TEXT NOT NULL DEFAULT '',
    primary_ip   TEXT NOT NULL DEFAULT '',
    oob_ip       TEXT NOT NULL DEFAULT '',
    serial       TEXT NOT NULL DEFAULT '',
    asset_tag    TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL
);

CREATE INDEX idx_devices_site ON devices(site);
CREATE INDEX idx_devices_owner ON devices(owner);

CREATE TABLE interfaces (
    id           TEXT PRIMARY KEY,
    device_id    TEXT NOT NULL,
    name         TEXT NOT NULL,
    mac_address  TEXT NOT NULL DEFAULT '',
    untagged_vlan INTEGER,
    payload_json TEXT NOT NULL,
    FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE
);

CREATE INDEX idx_interfaces_device ON interfaces(device_id);
CREATE INDEX idx_interfaces_mac ON interfaces(mac_address);

CREATE TABLE ip_addresses (
    id           TEXT PRIMARY KEY,
    address      TEXT NOT NULL,
    ip           TEXT NOT NULL,
    prefix       TEXT NOT NULL DEFAULT '',
    device_id    TEXT NOT NULL DEFAULT '',
    interface    TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL
);

CREATE INDEX idx_ip_prefix ON ip_addresses(prefix);
CREATE INDEX idx_ip_device ON ip_addresses(device_id);

CREATE TABLE device_module_bays (
    id           TEXT PRIMARY KEY,
    device_id    TEXT NOT NULL,
    name         TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE
);

CREATE TABLE device_modules (
    id           TEXT PRIMARY KEY,
    device_id    TEXT NOT NULL,
    bay          TEXT NOT NULL,
    module_type  TEXT NOT NULL,
    serial       TEXT NOT NULL DEFAULT '',
    asset_tag    TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL,
    FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE
);

CREATE TABLE vlans (
    id           TEXT PRIMARY KEY,
    site         TEXT NOT NULL,
    vid          INTEGER NOT NULL,
    name         TEXT NOT NULL,
    payload_json TEXT NOT NULL
);

CREATE TABLE prefixes (
    id           TEXT PRIMARY KEY,
    cidr         TEXT NOT NULL,
    site         TEXT NOT NULL,
    vlan         INTEGER,
    payload_json TEXT NOT NULL
);

CREATE TABLE cables (
    id           TEXT PRIMARY KEY,
    a_device_id  TEXT NOT NULL,
    a_port       TEXT NOT NULL,
    b_device_id  TEXT NOT NULL,
    b_port       TEXT NOT NULL,
    method       TEXT NOT NULL DEFAULT '',
    confidence   TEXT NOT NULL DEFAULT 'observed',
    payload_json TEXT NOT NULL
);

CREATE INDEX idx_cables_a ON cables(a_device_id);
CREATE INDEX idx_cables_b ON cables(b_device_id);

CREATE TABLE assets (
    id           TEXT PRIMARY KEY,
    asset_tag    TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    category     TEXT NOT NULL DEFAULT '',
    serial       TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL,
    site         TEXT NOT NULL DEFAULT '',
    owner        TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL
);

CREATE TABLE issues (
    id           TEXT PRIMARY KEY,
    severity     TEXT NOT NULL,
    kind         TEXT NOT NULL,
    entity_kind  TEXT NOT NULL DEFAULT '',
    entity_id    TEXT NOT NULL DEFAULT '',
    message      TEXT NOT NULL,
    payload_json TEXT NOT NULL
);

CREATE VIRTUAL TABLE search_fts USING fts5(
    entity_kind,
    entity_id,
    title,
    subtitle,
    body
);

CREATE TABLE tm_snapshots (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at       TEXT NOT NULL,
    last_verified_at TEXT NOT NULL,
    trigger          TEXT NOT NULL,
    trigger_detail   TEXT NOT NULL,
    state_hash       TEXT NOT NULL,
    summary_json     TEXT NOT NULL
);

CREATE INDEX idx_tm_snapshots_created ON tm_snapshots(created_at);

CREATE TABLE tm_blobs (
    hash TEXT PRIMARY KEY,
    data BLOB NOT NULL
);

CREATE TABLE tm_snapshot_entities (
    snapshot_id INTEGER NOT NULL,
    entity_kind TEXT NOT NULL,
    entity_id   TEXT NOT NULL,
    blob_hash   TEXT NOT NULL,
    PRIMARY KEY (snapshot_id, entity_kind, entity_id),
    FOREIGN KEY (snapshot_id) REFERENCES tm_snapshots(id) ON DELETE CASCADE
);

CREATE INDEX idx_tm_se_kind ON tm_snapshot_entities(snapshot_id, entity_kind);

CREATE TABLE tm_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    occurred_at TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    entity_kind TEXT NOT NULL,
    entity_id   TEXT NOT NULL,
    entity_name TEXT NOT NULL,
    summary     TEXT NOT NULL,
    diff_json   TEXT NOT NULL DEFAULT '[]',
    FOREIGN KEY (snapshot_id) REFERENCES tm_snapshots(id) ON DELETE CASCADE
);

CREATE INDEX idx_tm_events_occurred ON tm_events(occurred_at);
CREATE INDEX idx_tm_events_entity ON tm_events(entity_kind, entity_id);

-- +goose Down
DROP TABLE IF EXISTS tm_events;
DROP TABLE IF EXISTS tm_snapshot_entities;
DROP TABLE IF EXISTS tm_blobs;
DROP TABLE IF EXISTS tm_snapshots;
DROP TABLE IF EXISTS search_fts;
DROP TABLE IF EXISTS issues;
DROP TABLE IF EXISTS assets;
DROP TABLE IF EXISTS cables;
DROP TABLE IF EXISTS prefixes;
DROP TABLE IF EXISTS vlans;
DROP TABLE IF EXISTS device_modules;
DROP TABLE IF EXISTS device_module_bays;
DROP TABLE IF EXISTS ip_addresses;
DROP TABLE IF EXISTS interfaces;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS racks;
DROP TABLE IF EXISTS module_types;
DROP TABLE IF EXISTS device_types;
DROP TABLE IF EXISTS contacts;
DROP TABLE IF EXISTS owners;
DROP TABLE IF EXISTS owner_groups;
DROP TABLE IF EXISTS locations;
DROP TABLE IF EXISTS sites;
DROP TABLE IF EXISTS entity_identities;
DROP TABLE IF EXISTS manual_revisions;
DROP TABLE IF EXISTS raw_observations;
DROP TABLE IF EXISTS collector_source_status;
DROP TABLE IF EXISTS collectors;
