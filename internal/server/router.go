package server

import (
	"net/http"
	"strings"
)

// NewRouter constructs the HTTP handler with strictly read-only /api/v1 endpoints
// plus the collector and UI WebSocket endpoints.
func NewRouter(coord *Coordinator) http.Handler {
	h := &apiHandler{coord: coord}
	mux := http.NewServeMux()

	// WebSockets
	mux.Handle("/ws/collector", coord.CollectorHub())
	mux.Handle("/ws/events", coord.UIHub())

	// Read-only REST API
	mux.HandleFunc("GET /api/v1/health", h.health)
	mux.HandleFunc("GET /api/v1/overview", h.overview)
	mux.HandleFunc("GET /api/v1/topology", h.topology)

	mux.HandleFunc("GET /api/v1/sites", h.listSites)
	mux.HandleFunc("GET /api/v1/sites/{id}", h.getSite)

	mux.HandleFunc("GET /api/v1/racks", h.listRacks)
	mux.HandleFunc("GET /api/v1/racks/{id}", h.getRack)

	mux.HandleFunc("GET /api/v1/devices", h.listDevices)
	mux.HandleFunc("GET /api/v1/devices/{id}", h.getDevice)
	mux.HandleFunc("GET /api/v1/devices/{id}/history", h.getDeviceHistory)

	mux.HandleFunc("GET /api/v1/cables", h.listCables)
	mux.HandleFunc("GET /api/v1/cables/trace", h.traceCables)

	mux.HandleFunc("GET /api/v1/networks/vlans", h.listVLANs)
	mux.HandleFunc("GET /api/v1/networks/prefixes", h.listPrefixes)
	mux.HandleFunc("GET /api/v1/networks/prefixes/{id}/ips", h.getPrefixIPs)

	mux.HandleFunc("GET /api/v1/assets", h.listAssets)

	mux.HandleFunc("GET /api/v1/ownership/groups", h.listOwnerGroups)
	mux.HandleFunc("GET /api/v1/ownership/owners", h.listOwners)
	mux.HandleFunc("GET /api/v1/ownership/owners/{id}", h.getOwner)
	mux.HandleFunc("GET /api/v1/ownership/contacts", h.listContacts)

	mux.HandleFunc("GET /api/v1/search", h.search)

	mux.HandleFunc("GET /api/v1/timemachine/snapshots", h.listSnapshots)
	mux.HandleFunc("GET /api/v1/timemachine/events", h.listEvents)
	mux.HandleFunc("GET /api/v1/timemachine/diff", h.diffSnapshots)

	mux.HandleFunc("GET /api/v1/system/collectors", h.listCollectors)
	mux.HandleFunc("GET /api/v1/system/issues", h.listIssues)

	return readOnlyAPIMiddleware(mux)
}

func readOnlyAPIMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "Auto Netviz API is strictly read-only; edit manual YAML files to make changes.",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
