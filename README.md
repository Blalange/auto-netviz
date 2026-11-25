# Auto Netviz

**Auto Netviz** is a read-only infrastructure visualization platform for networks, cabling, racks, servers, inventory assets, and ownership. It combines declarative **manual YAML** files with live observations streamed from one or more remote **Go collectors** over outbound WebSockets, backed by a **SQLite + Goose** database with a **Time Machine** snapshot explorer and a **SvelteKit + shadcn-svelte** frontend.

## Architecture

- **`cmd/server` (`auto-netviz-server`)**: Go API & coordinator backend.
  - Watches a manual YAML directory (`fsnotify` + periodic hash fallback).
  - Accepts outbound WebSocket connections from remote collectors at `/ws/collector` (credentials never leave the collector).
  - Resolves cross-source identities (constrained Union-Find across MAC, IP, serial/service tag, UUID, hostname), auto-creates unmanaged devices & synthetic faceplates, detects manual conflicts and live drift, and emits bidirectional cables.
  - Persists canonical state and content-addressed zstd-compressed **Time Machine** snapshots in pure-Go SQLite (`modernc.org/sqlite`, WAL mode, FTS5, Goose embedded migrations).
  - Serves strictly read-only REST (`GET /api/v1/...`, with `?at=` point-in-time queries) and live UI WebSocket (`/ws/events`) on `:8080`.
- **`cmd/collector` (`auto-netviz-collector`)**: Go agent runnable inside remote VLANs, branches, or Kubernetes clusters.
  - Dials the server over WebSocket with exponential backoff and local disk spooling when disconnected.
  - Supports **UniFi**, **SNMP (v2c/v3)**, **LLDP** (passive `AF_PACKET` + `lldpctl`), **Redfish**, **IPMI**, **Discovery** (ARP/CIDR/OUI/DNS), **HostOS** (local + SSH), **Kubernetes** (`client-go` + optional `talosctl`), and `--demo` simulated telemetry.
- **`web/`**: SvelteKit 2 + Svelte 5 + Tailwind CSS v4 + `shadcn-svelte` SSR app (`@sveltejs/adapter-node`, `:3000`).
  - Zero authentication, strictly read-only UI, interactive parametric SVG rack elevations & device faceplates, live/historical SVG topology graph, BFS path/cable tracer, Time Machine scrubber & diff viewer, and one-click "Copy Manual YAML" modal.

## Quickstart (Local 3-Process)

1. Build binaries and frontend:
   ```bash
   go build -o bin/auto-netviz-server ./cmd/server
   go build -o bin/auto-netviz-collector ./cmd/collector
   cd web && npm ci && npm run build && cd ..
   ```

2. Start the backend server:
   ```bash
   ./bin/auto-netviz-server --manual ./examples/manual --db ./data/auto-netviz.db --listen :8080
   ```

3. Run a collector (`--demo` emits multi-source simulated inventory, switches, PDUs, servers, and cables):
   ```bash
   ./bin/auto-netviz-collector --demo --server-ws ws://127.0.0.1:8080/ws/collector --id demo-col --site hq
   ```

4. Start the SvelteKit frontend:
   ```bash
   BACKEND_INTERNAL_URL=http://127.0.0.1:8080 PORT=3000 HOST=0.0.0.0 node web/build/index.js
   ```
   Open `http://127.0.0.1:3000`.

## Docker Compose

```bash
docker compose -f docker/docker-compose.yml up --build
```

## Manual YAML Structure

Auto Netviz loads all `*.yaml` / `*.yml` files recursively from `--manual` (skipping dotfiles and Kubernetes `..data` symlink duplicates). See [`examples/manual/`](examples/manual) for complete examples:

- `schema_version`, `defaults` (`site`, `location`, `owner`, `rack`, `status`, `tags`)
- `sites`, `locations`, `owner_groups`, `owners`, `contacts`
- `device_types` (parametric `faceplate.front` / `faceplate.rear` port, SFP, PSU, and drive bay rows)
- `racks` (top-to-bottom `layout` solver supporting `device`, `face`, `gap`, and `empty` slots)
- `vlans`, `prefixes`
- `devices` (`identity`, `collect` locators, `interfaces`, `uplinks` shorthand, `owner`, `contacts`, `tags`)
- `cables`, `assets`, `rules` (`auto_create`, `ignore`)

## Collector Configuration

See [`examples/collector.yaml`](examples/collector.yaml). Credentials stay on the collector using secret refs (`from_env`, `from_file`, or `from_k8s_secret`):

```yaml
collector_id: branch-a
site: hq
schedules:
  unifi: 2m
  snmp: 5m
  lldp: 1m
  redfish: 15m
  ipmi: 15m
sources:
  unifi:
    - name: unifi-ctrl
      url: https://unifi.example.com
      username: { from_env: UNIFI_USER }
      password: { from_env: UNIFI_PASS }
      verify_tls: false
```

## Helm Chart

```bash
helm upgrade --install auto-netviz oci://ghcr.io/blalange/charts/auto-netviz \
  --version 0.1.0 \
  --namespace auto-netviz --create-namespace \
  -f my-values.yaml
```

Key chart values live in [`charts/auto-netviz/values.yaml`](charts/auto-netviz/values.yaml):

- `server.persistence` (`ReadWriteOnce` PVC for SQLite + `Recreate` Deployment strategy)
- `server.manual.existingConfigMap` or inline `server.manual.files`
- `collector.rbac.kubernetesReadOnly` & `collector.rbac.secretGrants` (least-privilege cross-namespace secret `RoleBinding`s)
- `collector.config` (rendered into `collector.yaml`)
- `ingress` (routes `/api` and `/ws` to the server service and `/` to the SvelteKit web service)
