package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/blalange/auto-netviz/internal/model"
)

type apiHandler struct {
	coord *Coordinator
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

func (h *apiHandler) stateForRequest(w http.ResponseWriter, r *http.Request) (*model.CanonicalState, *model.SnapshotSummary, bool) {
	at := strings.TrimSpace(r.URL.Query().Get("at"))
	if at == "" || strings.EqualFold(at, "live") {
		st, sum := h.coord.CurrentState()
		if st != nil {
			return st, sum, true
		}
	}
	st, sum, err := h.coord.store.LoadStateAt(r.Context(), at)
	if err != nil {
		status := http.StatusBadRequest
		if err == sql.ErrNoRows {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return nil, nil, false
	}
	return st, sum, true
}

func (h *apiHandler) health(w http.ResponseWriter, r *http.Request) {
	st, sum := h.coord.CurrentState()
	cols, _ := h.coord.store.ListCollectors(r.Context())
	online := 0
	for _, c := range cols {
		if c.Status == "online" {
			online++
		}
	}
	var snapID int64
	if sum != nil {
		snapID = sum.ID
	}
	devCount := 0
	if st != nil {
		devCount = len(st.Devices)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "ok",
		"snapshot_id":       snapID,
		"devices":           devCount,
		"collectors_online": online,
		"collectors_total":  len(cols),
	})
}

func (h *apiHandler) overview(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	cols, _ := h.coord.store.ListCollectors(r.Context())
	events, _ := h.coord.store.ListEvents(r.Context(), "", "", 20)

	autoCount := 0
	statusCounts := make(map[string]int)
	roleCounts := make(map[string]int)
	for _, d := range st.Devices {
		if d.Origin == "auto" {
			autoCount++
		}
		statusCounts[d.Status]++
		roleCounts[d.Role]++
	}
	issueCounts := make(map[string]int)
	for _, iss := range st.Issues {
		issueCounts[iss.Severity]++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot": sum,
		"counts": model.SnapshotCounts{
			Sites:       len(st.Sites),
			Racks:       len(st.Racks),
			Devices:     len(st.Devices),
			AutoDevices: autoCount,
			Cables:      len(st.Cables),
			VLANs:       len(st.VLANs),
			Prefixes:    len(st.Prefixes),
			Assets:      len(st.Assets),
			Owners:      len(st.Owners),
			Issues:      len(st.Issues),
		},
		"status_breakdown": statusCounts,
		"role_breakdown":   roleCounts,
		"issue_breakdown":  issueCounts,
		"sites":            st.Sites,
		"collectors":       cols,
		"recent_events":    events,
	})
}

func (h *apiHandler) topology(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	siteFilter := q.Get("site")
	roleFilter := q.Get("role")
	ownerFilter := q.Get("owner")
	vlanStr := q.Get("vlan")
	layer := q.Get("layer")
	if layer == "" {
		layer = "all"
	}

	var vlanFilter int
	if vlanStr != "" {
		vlanFilter, _ = strconv.Atoi(vlanStr)
	}

	included := make(map[string]bool)
	nodes := make([]map[string]any, 0, len(st.Devices))
	for _, d := range st.Devices {
		if siteFilter != "" && d.Site != siteFilter {
			continue
		}
		if roleFilter != "" && d.Role != roleFilter {
			continue
		}
		if ownerFilter != "" && d.Owner != ownerFilter {
			continue
		}
		if vlanFilter > 0 {
			hasVLAN := false
			for _, iface := range d.Interfaces {
				if iface.UntaggedVLAN != nil && *iface.UntaggedVLAN == vlanFilter {
					hasVLAN = true
					break
				}
				for _, tv := range iface.TaggedVLANs {
					if tv == vlanFilter {
						hasVLAN = true
						break
					}
				}
			}
			if !hasVLAN {
				continue
			}
		}
		included[d.ID] = true
		nodes = append(nodes, map[string]any{
			"id":          d.ID,
			"name":        d.Name,
			"origin":      d.Origin,
			"site":        d.Site,
			"role":        d.Role,
			"role_color":  d.RoleColor,
			"status":      d.Status,
			"device_type": d.DeviceType,
			"primary_ip":  d.PrimaryIP,
			"oob_ip":      d.OOBIP,
			"owner":       d.Owner,
			"rack":        d.Rack,
			"sources":     d.Sources,
			"port_count":  len(d.Interfaces),
		})
	}

	var edges []model.Cable
	for _, c := range st.Cables {
		if !included[c.ADeviceID] || !included[c.BDeviceID] {
			continue
		}
		if layer == "physical" && c.Confidence == "inferred" {
			continue
		}
		if layer == "l2" && c.Method == "manual" && c.Type == "power" {
			continue
		}
		edges = append(edges, c)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot": sum,
		"nodes":    nodes,
		"edges":    edges,
		"sites":    st.Sites,
		"vlans":    st.VLANs,
	})
}

func (h *apiHandler) listSites(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "sites": st.Sites, "locations": st.Locations})
}

func (h *apiHandler) getSite(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	for _, s := range st.Sites {
		if s.ID == id {
			var racks []model.Rack
			for _, rk := range st.Racks {
				if rk.Site == s.ID {
					racks = append(racks, rk)
				}
			}
			var devs []model.Device
			for _, d := range st.Devices {
				if d.Site == s.ID {
					devs = append(devs, d)
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "site": s, "racks": racks, "devices": devs})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "site not found"})
}

func (h *apiHandler) listRacks(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	site := r.URL.Query().Get("site")
	var out []model.Rack
	for _, rk := range st.Racks {
		if site != "" && rk.Site != site {
			continue
		}
		out = append(out, rk)
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "racks": out})
}

func (h *apiHandler) getRack(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var target *model.Rack
	for i := range st.Racks {
		if st.Racks[i].ID == id || st.Racks[i].Name == id {
			target = &st.Racks[i]
			break
		}
	}
	if target == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "rack not found"})
		return
	}

	dtMap := make(map[string]model.DeviceType, len(st.DeviceTypes))
	for _, dt := range st.DeviceTypes {
		dtMap[dt.ID] = dt
	}

	rackedIDs := make(map[string]bool)
	var rackedDevices []model.Device
	var unrackedDevices []model.Device
	for _, d := range st.Devices {
		if d.RackID == target.ID || (d.Site == target.Site && d.Rack == target.Name) {
			rackedIDs[d.ID] = true
			rackedDevices = append(rackedDevices, d)
		} else if d.Site == target.Site && d.Rack == "" {
			unrackedDevices = append(unrackedDevices, d)
		}
	}

	var cables []model.Cable
	for _, c := range st.Cables {
		if rackedIDs[c.ADeviceID] || rackedIDs[c.BDeviceID] {
			cables = append(cables, c)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot":         sum,
		"rack":             target,
		"devices":          rackedDevices,
		"unracked_devices": unrackedDevices,
		"device_types":     dtMap,
		"cables":           cables,
	})
}

func (h *apiHandler) listDevices(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	site := q.Get("site")
	role := q.Get("role")
	owner := q.Get("owner")
	origin := q.Get("origin")
	status := q.Get("status")
	rack := q.Get("rack")
	source := q.Get("source")

	var out []model.Device
	for _, d := range st.Devices {
		if site != "" && d.Site != site {
			continue
		}
		if role != "" && d.Role != role {
			continue
		}
		if owner != "" && d.Owner != owner {
			continue
		}
		if origin != "" && d.Origin != origin {
			continue
		}
		if status != "" && d.Status != status {
			continue
		}
		if rack != "" && d.Rack != rack && d.RackID != rack {
			continue
		}
		if source != "" && !containsString(d.Sources, source) {
			continue
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot":     sum,
		"devices":      out,
		"device_types": st.DeviceTypes,
	})
}

func (h *apiHandler) getDevice(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var dev *model.Device
	for i := range st.Devices {
		if st.Devices[i].ID == id || st.Devices[i].Name == id {
			dev = &st.Devices[i]
			break
		}
	}
	if dev == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}

	var dt *model.DeviceType
	for i := range st.DeviceTypes {
		if st.DeviceTypes[i].ID == dev.DeviceType {
			dt = &st.DeviceTypes[i]
			break
		}
	}

	var cables []model.Cable
	for _, c := range st.Cables {
		if c.ADeviceID == dev.ID || c.BDeviceID == dev.ID {
			cables = append(cables, c)
		}
	}

	var devIssues []model.Issue
	for _, iss := range st.Issues {
		if iss.EntityID == dev.ID || iss.EntityName == dev.Name {
			devIssues = append(devIssues, iss)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot":    sum,
		"device":      dev,
		"device_type": dt,
		"cables":      cables,
		"issues":      devIssues,
	})
}

func (h *apiHandler) getDeviceHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	events, err := h.coord.store.ListEvents(r.Context(), "device", id, 200)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device_id": id, "events": events})
}

func (h *apiHandler) listCables(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	method := q.Get("method")
	conf := q.Get("confidence")
	dev := q.Get("device")

	var out []model.Cable
	for _, c := range st.Cables {
		if method != "" && c.Method != method {
			continue
		}
		if conf != "" && c.Confidence != conf {
			continue
		}
		if dev != "" && c.ADeviceID != dev && c.BDeviceID != dev && c.ADevice != dev && c.BDevice != dev {
			continue
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "cables": out})
}

func (h *apiHandler) traceCables(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	fromQ := strings.TrimSpace(r.URL.Query().Get("from"))
	toQ := strings.TrimSpace(r.URL.Query().Get("to"))
	if fromQ == "" || toQ == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "'from' and 'to' query params are required"})
		return
	}

	resolveID := func(ref string) string {
		for _, d := range st.Devices {
			if d.ID == ref || strings.EqualFold(d.Name, ref) {
				return d.ID
			}
		}
		return ""
	}
	fromID, toID := resolveID(fromQ), resolveID(toQ)
	if fromID == "" || toID == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}

	type adjEdge struct {
		nextID string
		cable  model.Cable
	}
	adj := make(map[string][]adjEdge)
	for _, c := range st.Cables {
		adj[c.ADeviceID] = append(adj[c.ADeviceID], adjEdge{nextID: c.BDeviceID, cable: c})
		adj[c.BDeviceID] = append(adj[c.BDeviceID], adjEdge{nextID: c.ADeviceID, cable: c})
	}

	type queueItem struct {
		devID string
		path  []model.Cable
	}
	visited := map[string]bool{fromID: true}
	queue := []queueItem{{devID: fromID}}
	var foundPath []model.Cable

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.devID == toID {
			foundPath = cur.path
			break
		}
		for _, edge := range adj[cur.devID] {
			if visited[edge.nextID] {
				continue
			}
			visited[edge.nextID] = true
			nextPath := append(append([]model.Cable(nil), cur.path...), edge.cable)
			queue = append(queue, queueItem{devID: edge.nextID, path: nextPath})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot": sum,
		"from_id":  fromID,
		"to_id":    toID,
		"found":    len(foundPath) > 0 || fromID == toID,
		"hops":     foundPath,
	})
}

func (h *apiHandler) listVLANs(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	site := r.URL.Query().Get("site")
	var out []model.VLAN
	for _, v := range st.VLANs {
		if site != "" && v.Site != site {
			continue
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "vlans": out})
}

func (h *apiHandler) listPrefixes(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	site := r.URL.Query().Get("site")
	var out []model.Prefix
	for _, p := range st.Prefixes {
		if site != "" && p.Site != site {
			continue
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "prefixes": out})
}

func (h *apiHandler) getPrefixIPs(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var target *model.Prefix
	for i := range st.Prefixes {
		if st.Prefixes[i].ID == id || st.Prefixes[i].CIDR == id {
			target = &st.Prefixes[i]
			break
		}
	}
	if target == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "prefix not found"})
		return
	}
	netPfx, err := netip.ParsePrefix(target.CIDR)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var ips []model.IPAddress
	for _, d := range st.Devices {
		for _, iface := range d.Interfaces {
			for _, ip := range iface.IPAddresses {
				if addr, err := netip.ParseAddr(ip.IP); err == nil && netPfx.Contains(addr) {
					ips = append(ips, ip)
				}
			}
		}
	}
	sort.Slice(ips, func(i, j int) bool { return ips[i].IP < ips[j].IP })
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "prefix": target, "ip_addresses": ips})
}

func (h *apiHandler) listAssets(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	kind := q.Get("kind")
	site := q.Get("site")
	owner := q.Get("owner")
	status := q.Get("status")

	var out []model.Asset
	for _, a := range st.Assets {
		if kind != "" && a.Kind != kind {
			continue
		}
		if site != "" && a.Site != site {
			continue
		}
		if owner != "" && a.Owner != owner {
			continue
		}
		if status != "" && a.Status != status {
			continue
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "assets": out})
}

func (h *apiHandler) listOwnerGroups(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "owner_groups": st.OwnerGroups})
}

func (h *apiHandler) listOwners(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	devCounts := make(map[string]int)
	assetCounts := make(map[string]int)
	for _, d := range st.Devices {
		if d.Owner != "" {
			devCounts[d.Owner]++
		}
	}
	for _, a := range st.Assets {
		if a.Owner != "" && a.Kind == "standalone" {
			assetCounts[a.Owner]++
		}
	}

	type ownerSummary struct {
		model.Owner
		DeviceCount int `json:"device_count"`
		AssetCount  int `json:"asset_count"`
	}
	out := make([]ownerSummary, 0, len(st.Owners))
	for _, o := range st.Owners {
		out = append(out, ownerSummary{
			Owner:       o,
			DeviceCount: devCounts[o.Name],
			AssetCount:  assetCounts[o.Name],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "owners": out, "owner_groups": st.OwnerGroups})
}

func (h *apiHandler) getOwner(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var owner *model.Owner
	for i := range st.Owners {
		if st.Owners[i].ID == id || strings.EqualFold(st.Owners[i].Name, id) {
			owner = &st.Owners[i]
			break
		}
	}
	if owner == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "owner not found"})
		return
	}
	var devs []model.Device
	for _, d := range st.Devices {
		if d.Owner == owner.Name {
			devs = append(devs, d)
		}
	}
	var assets []model.Asset
	for _, a := range st.Assets {
		if a.Owner == owner.Name {
			assets = append(assets, a)
		}
	}
	var contacts []model.Contact
	for _, c := range st.Contacts {
		if c.Owner == owner.Name {
			contacts = append(contacts, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"snapshot": sum,
		"owner":    owner,
		"devices":  devs,
		"assets":   assets,
		"contacts": contacts,
	})
}

func (h *apiHandler) listContacts(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "contacts": st.Contacts})
}

func (h *apiHandler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	at := r.URL.Query().Get("at")
	results, err := h.coord.store.Search(r.Context(), q, at, 30)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": q, "results": results})
}

func (h *apiHandler) listSnapshots(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	snaps, total, err := h.coord.store.ListSnapshots(r.Context(), limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": snaps, "total": total})
}

func (h *apiHandler) listEvents(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("entity_kind")
	id := r.URL.Query().Get("entity_id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := h.coord.store.ListEvents(r.Context(), kind, id, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *apiHandler) diffSnapshots(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "'from' and 'to' query params are required"})
		return
	}
	diff, err := h.coord.store.DiffSnapshots(r.Context(), from, to)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

func (h *apiHandler) listCollectors(w http.ResponseWriter, r *http.Request) {
	cols, err := h.coord.store.ListCollectors(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collectors": cols})
}

func (h *apiHandler) listIssues(w http.ResponseWriter, r *http.Request) {
	st, sum, ok := h.stateForRequest(w, r)
	if !ok {
		return
	}
	sev := r.URL.Query().Get("severity")
	kind := r.URL.Query().Get("kind")
	var out []model.Issue
	for _, iss := range st.Issues {
		if sev != "" && iss.Severity != sev {
			continue
		}
		if kind != "" && iss.Kind != kind {
			continue
		}
		out = append(out, iss)
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": sum, "issues": out})
}

func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}
