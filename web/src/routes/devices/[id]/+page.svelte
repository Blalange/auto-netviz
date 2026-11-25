<script lang="ts">
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import Badge from '$lib/components/ui/badge/Badge.svelte';
	import Button from '$lib/components/ui/button/Button.svelte';
	import OriginBadge from '$lib/components/shared/OriginBadge.svelte';
	import ProvenanceBadges from '$lib/components/shared/ProvenanceBadges.svelte';
	import CopyYamlModal from '$lib/components/shared/CopyYamlModal.svelte';
	import DeviceFaceplate from '$lib/components/svg/DeviceFaceplate.svelte';
	import { formatTime, statusColorClass, withAtParam } from '$lib/utils';
	import { AlertTriangle, Code2 } from '@lucide/svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	let d = $derived(data.device);
	let yamlOpen = $state(false);
	let activeTab = $state('interfaces');
	let selectedPort = $state<string | null>(null);
</script>

<CopyYamlModal bind:open={yamlOpen} deviceName={d.name} yamlSnippet={d.manual_yaml_snippet || ''} />

<div class="space-y-6">
	<div class="flex flex-wrap items-start justify-between gap-4">
		<div>
			<div class="text-xs text-muted-foreground mb-1">
				<a href={withAtParam('/devices', data.at)} class="hover:underline">Devices</a> / {d.site} / {d.role}
			</div>
			<div class="flex flex-wrap items-center gap-2.5">
				<span class={`h-3 w-3 rounded-full ${statusColorClass(d.status)}`}></span>
				<h1 class="text-2xl font-bold tracking-tight">{d.name}</h1>
				<OriginBadge origin={d.origin} />
				<ProvenanceBadges sources={d.sources} />
			</div>
			{#if d.description}
				<p class="mt-1 text-sm text-muted-foreground">{d.description}</p>
			{/if}
		</div>

		<Button variant="outline" size="sm" onclick={() => (yamlOpen = true)}>
			<Code2 class="h-4 w-4" />
			Copy Manual YAML
		</Button>
	</div>

	{#if (data.issues ?? []).length > 0}
		<div class="rounded-xl border border-amber-500/40 bg-amber-500/10 p-4 space-y-1.5">
			<div class="flex items-center gap-2 text-xs font-semibold text-amber-600 dark:text-amber-300">
				<AlertTriangle class="h-4 w-4" />
				Active Drift / Merge Alerts ({data.issues.length})
			</div>
			{#each data.issues as iss}
				<div class="text-xs font-mono text-foreground/90">• {iss.message}</div>
			{/each}
		</div>
	{/if}

	<!-- Key Identity & Specs Grid -->
	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
		<Card>
			<CardHeader class="pb-1"><CardTitle class="text-xs text-muted-foreground">Hardware Model</CardTitle></CardHeader>
			<CardContent class="space-y-1 text-xs">
				<div class="text-sm font-semibold">{d.manufacturer || '—'} {d.model || ''}</div>
				<div class="font-mono text-muted-foreground">Type: {d.device_type}</div>
				{#if d.serial}<div class="font-mono">Serial: {d.serial}</div>{/if}
				{#if d.system_uuid}<div class="font-mono text-[10px] truncate" title={d.system_uuid}>UUID: {d.system_uuid}</div>{/if}
			</CardContent>
		</Card>

		<Card>
			<CardHeader class="pb-1"><CardTitle class="text-xs text-muted-foreground">Management & OS</CardTitle></CardHeader>
			<CardContent class="space-y-1 text-xs font-mono">
				<div>Primary IP: <span class="font-semibold">{d.primary_ip || '—'}</span></div>
				<div>OOB / BMC: <span class="font-semibold">{d.oob_ip || '—'}</span></div>
				{#if d.os_version}<div class="font-sans text-muted-foreground">OS: {d.os_version}</div>{/if}
				{#if d.firmware_version}<div class="text-muted-foreground">FW: {d.firmware_version}</div>{/if}
			</CardContent>
		</Card>

		<Card>
			<CardHeader class="pb-1"><CardTitle class="text-xs text-muted-foreground">Physical Placement</CardTitle></CardHeader>
			<CardContent class="space-y-1 text-xs">
				<div>Site: <span class="font-semibold">{d.site}</span></div>
				{#if d.rack}
					<div>
						Rack:
						<a href={withAtParam(`/racks/${encodeURIComponent(d.rack_id || d.rack)}`, data.at)} class="font-semibold text-primary hover:underline">
							{d.rack}
						</a>
						<span class="font-mono text-muted-foreground">(U{d.position}–U{d.end_position}, {d.face})</span>
					</div>
				{:else}
					<div class="text-muted-foreground">Unracked</div>
				{/if}
				{#if d.power_state}<div class="font-mono">Power: {d.power_state}</div>{/if}
			</CardContent>
		</Card>

		<Card>
			<CardHeader class="pb-1"><CardTitle class="text-xs text-muted-foreground">Ownership & Asset</CardTitle></CardHeader>
			<CardContent class="space-y-1 text-xs">
				<div>
					Owner:
					{#if d.owner}
						<a href={withAtParam('/ownership', data.at)} class="font-semibold text-primary hover:underline">{d.owner}</a>
					{:else}
						<span class="text-muted-foreground">Unassigned</span>
					{/if}
				</div>
				{#if d.asset_tag}<div class="font-mono">Asset Tag: {d.asset_tag}</div>{/if}
				{#if d.warranty_until}<div class="font-mono">Warranty: {d.warranty_until}</div>{/if}
				{#each d.contacts ?? [] as ct}
					<div class="text-muted-foreground">{ct.contact} ({ct.role || 'contact'})</div>
				{/each}
			</CardContent>
		</Card>
	</div>

	<!-- Parametric SVG Front & Rear Faceplates -->
	<Card>
		<CardHeader>
			<CardTitle>Parametric Device Elevation</CardTitle>
			<CardDescription>Click any port or bay to inspect live link speed, VLANs, PoE draw, and attached cable</CardDescription>
		</CardHeader>
		<CardContent class="grid gap-6 lg:grid-cols-2">
			<div>
				<div class="mb-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">Front Panel</div>
				<DeviceFaceplate device={d} deviceType={data.device_type} side="front" cables={data.cables ?? []} bind:selectedPort />
			</div>
			<div>
				<div class="mb-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">Rear Panel</div>
				<DeviceFaceplate device={d} deviceType={data.device_type} side="rear" cables={data.cables ?? []} bind:selectedPort />
			</div>
		</CardContent>
	</Card>

	<!-- Detail Tabs -->
	<Tabs bind:value={activeTab}>
		<TabsList>
			<TabsTrigger value="interfaces">Interfaces ({(d.interfaces ?? []).length})</TabsTrigger>
			<TabsTrigger value="modules">Hardware Modules ({(d.modules ?? []).length})</TabsTrigger>
			<TabsTrigger value="cables">Cables ({(data.cables ?? []).length})</TabsTrigger>
			<TabsTrigger value="provenance">Field Provenance ({(d.provenance ?? []).length})</TabsTrigger>
			<TabsTrigger value="history">History ({(data.history ?? []).length})</TabsTrigger>
		</TabsList>

		<TabsContent value="interfaces">
			<Card>
				<CardContent class="pt-4">
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Port</TableHead>
								<TableHead>Type & Speed</TableHead>
								<TableHead>MAC</TableHead>
								<TableHead>VLANs</TableHead>
								<TableHead>IP Addresses</TableHead>
								<TableHead>Sources</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{#each d.interfaces ?? [] as iface}
								<TableRow>
									<TableCell class="font-mono text-xs font-semibold">
										<div class="flex items-center gap-2">
											<span class={`h-2 w-2 rounded-full ${iface.link_up ? 'bg-emerald-500' : iface.enabled ? 'bg-slate-400' : 'bg-rose-500'}`}></span>
											{iface.name}
										</div>
										{#if iface.description}
											<div class="font-sans font-normal text-[11px] text-muted-foreground">{iface.description}</div>
										{/if}
									</TableCell>
									<TableCell class="font-mono text-xs">
										<div>{iface.type}</div>
										{#if iface.speed_mbps}<div class="text-emerald-600 dark:text-emerald-400">{iface.speed_mbps} Mbps</div>{/if}
										{#if iface.poe_watts}<div class="text-amber-600 dark:text-amber-400">{iface.poe_watts}W PoE</div>{/if}
									</TableCell>
									<TableCell class="font-mono text-xs">{iface.mac_address || '—'}</TableCell>
									<TableCell class="font-mono text-xs">
										{#if iface.untagged_vlan}<Badge variant="outline">PVID {iface.untagged_vlan}</Badge>{/if}
										{#if iface.tagged_vlans && iface.tagged_vlans.length > 0}
											<div class="mt-1 text-[10px] text-muted-foreground">Tagged: {iface.tagged_vlans.join(', ')}</div>
										{/if}
									</TableCell>
									<TableCell class="font-mono text-xs">
										{#each iface.ip_addresses ?? [] as ip}
											<div>{ip.address} {ip.primary ? '★' : ''}</div>
										{/each}
									</TableCell>
									<TableCell><ProvenanceBadges sources={iface.sources} /></TableCell>
								</TableRow>
							{/each}
						</TableBody>
					</Table>
				</CardContent>
			</Card>
		</TabsContent>

		<TabsContent value="modules">
			<Card>
				<CardContent class="pt-4">
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Bay</TableHead>
								<TableHead>Profile</TableHead>
								<TableHead>Model</TableHead>
								<TableHead>Serial</TableHead>
								<TableHead>Status</TableHead>
								<TableHead>Sources</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{#each d.modules ?? [] as mod}
								<TableRow>
									<TableCell class="font-mono text-xs font-semibold">{mod.bay}</TableCell>
									<TableCell class="text-xs">{mod.profile || '—'}</TableCell>
									<TableCell class="text-xs">
										<div>{mod.manufacturer || ''} {mod.model || mod.module_type}</div>
										{#if mod.description}<div class="text-muted-foreground">{mod.description}</div>{/if}
									</TableCell>
									<TableCell class="font-mono text-xs">{mod.serial || '—'}</TableCell>
									<TableCell><Badge variant={mod.status === 'active' ? 'success' : 'destructive'}>{mod.status}</Badge></TableCell>
									<TableCell><ProvenanceBadges sources={mod.sources} /></TableCell>
								</TableRow>
							{/each}
						</TableBody>
					</Table>
				</CardContent>
			</Card>
		</TabsContent>

		<TabsContent value="cables">
			<Card>
				<CardContent class="pt-4">
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Local Port</TableHead>
								<TableHead>Peer Device & Port</TableHead>
								<TableHead>Type</TableHead>
								<TableHead>Confidence</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{#each data.cables ?? [] as c}
								{@const isA = c.a_device_id === d.id}
								<TableRow>
									<TableCell class="font-mono text-xs font-semibold">{isA ? c.a_port : c.b_port}</TableCell>
									<TableCell class="font-mono text-xs">
										<a
											href={withAtParam(`/devices/${encodeURIComponent(isA ? c.b_device_id : c.a_device_id)}`, data.at)}
											class="text-primary hover:underline font-semibold"
										>
											{isA ? c.b_device : c.a_device}
										</a>
										: {isA ? c.b_port : c.a_port}
									</TableCell>
									<TableCell class="font-mono text-xs">{c.type}</TableCell>
									<TableCell><Badge variant="outline">{c.confidence}</Badge></TableCell>
								</TableRow>
							{/each}
						</TableBody>
					</Table>
				</CardContent>
			</Card>
		</TabsContent>

		<TabsContent value="provenance">
			<Card>
				<CardContent class="pt-4">
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Field</TableHead>
								<TableHead>Winning Source</TableHead>
								<TableHead>Collector</TableHead>
								<TableHead>Manual Override</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{#each d.provenance ?? [] as pv}
								<TableRow>
									<TableCell class="font-mono text-xs font-semibold">{pv.field}</TableCell>
									<TableCell><ProvenanceBadges sources={[pv.source]} /></TableCell>
									<TableCell class="font-mono text-xs text-muted-foreground">{pv.collector_id || '—'}</TableCell>
									<TableCell class="text-xs">{pv.manual_override ? 'Yes (differs from live)' : 'No'}</TableCell>
								</TableRow>
							{/each}
						</TableBody>
					</Table>
				</CardContent>
			</Card>
		</TabsContent>

		<TabsContent value="history">
			<Card>
				<CardContent class="pt-4">
					{#if (data.history ?? []).length === 0}
						<div class="p-6 text-center text-xs text-muted-foreground">No Time Machine changes recorded for this device yet.</div>
					{:else}
						<div class="divide-y">
							{#each data.history ?? [] as ev}
								<div class="py-2.5 flex items-center justify-between gap-3 text-xs">
									<div>
										<span class="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] mr-2">{ev.event_type}</span>
										<span>{ev.summary}</span>
									</div>
									<a href={withAtParam(`/devices/${encodeURIComponent(d.id)}`, String(ev.snapshot_id))} class="font-mono text-primary hover:underline shrink-0">
										Snapshot #{ev.snapshot_id} · {formatTime(ev.occurred_at)}
									</a>
								</div>
							{/each}
						</div>
					{/if}
				</CardContent>
			</Card>
		</TabsContent>
	</Tabs>
</div>
