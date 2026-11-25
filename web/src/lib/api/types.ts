export interface SnapshotCounts {
	sites: number;
	racks: number;
	devices: number;
	auto_devices: number;
	cables: number;
	vlans: number;
	prefixes: number;
	assets: number;
	owners: number;
	issues: number;
}

export interface SnapshotSummary {
	id: number;
	created_at: string;
	last_verified_at: string;
	trigger: string;
	trigger_detail: string;
	state_hash: string;
	counts: SnapshotCounts;
	event_count: number;
}

export interface TimelineEvent {
	id: number;
	snapshot_id: number;
	occurred_at: string;
	event_type: string;
	entity_kind: string;
	entity_id: string;
	entity_name: string;
	summary: string;
	diff_json?: string;
}

export interface Site {
	id: string;
	name: string;
	status: string;
	facility?: string;
	address?: string;
	description?: string;
	sources: string[];
}

export interface FaceplateElement {
	kind: string;
	label?: string;
	connector?: string;
	form_factor?: string;
	rows?: number;
	cols?: number;
	start?: number;
	count?: number;
	width?: number;
	prefix?: string;
	names?: string[];
	poe?: boolean;
}

export interface FaceplateSpec {
	front?: FaceplateElement[];
	rear?: FaceplateElement[];
}

export interface DeviceType {
	id: string;
	manufacturer: string;
	model: string;
	u_height: number;
	depth?: string;
	role_hint?: string;
	part_number?: string;
	airflow?: string;
	weight_kg?: number;
	description?: string;
	faceplate?: FaceplateSpec;
	auto_generated?: boolean;
	sources: string[];
}

export interface RackSlot {
	device_id: string;
	device_name: string;
	role: string;
	device_type: string;
	start_u: number;
	end_u: number;
	u_height: number;
	face: 'front' | 'rear';
	status: string;
}

export interface Rack {
	id: string;
	name: string;
	site: string;
	location?: string;
	status: string;
	width: number;
	u_height: number;
	desc_units?: boolean;
	power_budget_w?: number;
	power_draw_w?: number;
	used_u: number;
	description?: string;
	slots?: RackSlot[];
	sources: string[];
}

export interface IPAddress {
	id: string;
	address: string;
	ip: string;
	prefix?: string;
	dns_name?: string;
	description?: string;
	role?: string;
	status: string;
	primary?: boolean;
	device_id?: string;
	device_name?: string;
	interface?: string;
	sources: string[];
}

export interface InterfaceItem {
	id: string;
	name: string;
	type: string;
	kind?: string;
	enabled: boolean;
	link_up?: boolean;
	mgmt_only?: boolean;
	speed_mbps?: number;
	mtu?: number;
	mac_address?: string;
	description?: string;
	mode?: string;
	untagged_vlan?: number;
	tagged_vlans?: number[];
	poe_mode?: string;
	poe_watts?: number;
	ip_addresses?: IPAddress[];
	sources: string[];
}

export interface DeviceModule {
	id: string;
	bay: string;
	module_type: string;
	profile?: string;
	manufacturer?: string;
	model?: string;
	status: string;
	serial?: string;
	asset_tag?: string;
	description?: string;
	sources: string[];
}

export interface FieldProvenance {
	field: string;
	source: string;
	collector_id?: string;
	observed_at?: string;
	manual_override?: boolean;
}

export interface Device {
	id: string;
	name: string;
	origin: 'manual' | 'merged' | 'auto';
	site: string;
	location?: string;
	role: string;
	role_color?: string;
	device_type: string;
	manufacturer?: string;
	model?: string;
	platform?: string;
	status: string;
	power_state?: string;
	rack?: string;
	rack_id?: string;
	position?: number;
	end_position?: number;
	face?: string;
	serial?: string;
	service_tag?: string;
	system_uuid?: string;
	asset_tag?: string;
	warranty_until?: string;
	owner?: string;
	contacts?: { contact: string; role?: string }[];
	primary_ip?: string;
	oob_ip?: string;
	firmware_version?: string;
	os_version?: string;
	kernel_version?: string;
	description?: string;
	comments?: string;
	tags?: string[];
	sources: string[];
	collector_ids?: string[];
	interfaces?: InterfaceItem[];
	modules?: DeviceModule[];
	custom?: Record<string, unknown>;
	provenance?: FieldProvenance[];
	manual_yaml_snippet?: string;
	first_seen_at?: string;
	last_seen_at?: string;
}

export interface Cable {
	id: string;
	a_device_id: string;
	a_device: string;
	a_port: string;
	b_device_id: string;
	b_device: string;
	b_port: string;
	type: string;
	status: string;
	method?: string;
	confidence: 'manual' | 'verified' | 'observed' | 'inferred';
	speed_mbps?: number;
	color?: string;
	label?: string;
	description?: string;
	sources: string[];
}

export interface VLAN {
	id: string;
	site: string;
	vid: number;
	name: string;
	status: string;
	color?: string;
	description?: string;
	sources: string[];
}

export interface Prefix {
	id: string;
	cidr: string;
	site: string;
	vlan?: number;
	gateway?: string;
	status: string;
	description?: string;
	used_ips: number;
	total_hosts: number;
	utilization: number;
	sources: string[];
}

export interface Asset {
	id: string;
	asset_tag?: string;
	name: string;
	kind: 'standalone' | 'device' | 'module';
	category?: string;
	manufacturer?: string;
	model?: string;
	serial?: string;
	status: string;
	site?: string;
	location?: string;
	device_id?: string;
	device_name?: string;
	owner?: string;
	purchase_date?: string;
	warranty_until?: string;
	notes?: string;
	sources: string[];
}

export interface Owner {
	id: string;
	name: string;
	group?: string;
	email?: string;
	link?: string;
	description?: string;
	device_count?: number;
	asset_count?: number;
	sources: string[];
}

export interface OwnerGroup {
	id: string;
	name: string;
	description?: string;
}

export interface Contact {
	id: string;
	name: string;
	role?: string;
	owner?: string;
	email?: string;
	phone?: string;
	notes?: string;
}

export interface Issue {
	id: string;
	severity: 'error' | 'warning' | 'info';
	kind: string;
	entity_kind?: string;
	entity_id?: string;
	entity_name?: string;
	field?: string;
	message: string;
	detail?: string;
	file_path?: string;
	detected_at: string;
}

export interface CollectorInfo {
	collector_id: string;
	name: string;
	site: string;
	zone?: string;
	version: string;
	hostname?: string;
	status: 'online' | 'offline' | 'degraded';
	last_seen_at: string;
	connected_at?: string;
	uptime_seconds: number;
	queue_depth: number;
	sources?: {
		source: string;
		profile: string;
		enabled: boolean;
		status: string;
		last_run_at?: string;
		last_success_at?: string;
		duration_ms: number;
		device_count: number;
		link_count: number;
		error?: string;
	}[];
}

export interface FieldDiff {
	field: string;
	old?: unknown;
	new?: unknown;
}

export interface EntityDiff {
	entity_kind: string;
	entity_id: string;
	name: string;
	change: 'added' | 'removed' | 'modified';
	fields?: FieldDiff[];
	before?: Record<string, unknown>;
	after?: Record<string, unknown>;
}

export interface SnapshotDiff {
	from_snapshot: SnapshotSummary;
	to_snapshot: SnapshotSummary;
	added_count: number;
	removed_count: number;
	modified_count: number;
	entities: EntityDiff[];
}

export interface SearchResult {
	entity_kind: string;
	entity_id: string;
	title: string;
	subtitle: string;
}
