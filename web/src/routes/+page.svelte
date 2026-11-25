<script lang="ts">
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import Badge from '$lib/components/ui/badge/Badge.svelte';
	import { formatRelative, statusColorClass, withAtParam } from '$lib/utils';
	import { Server, Cable, HardDrive, Package, Users, AlertTriangle, Activity, Sparkles } from '@lucide/svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	let ov = $derived(data.overview);
	let counts = $derived(ov.counts);
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight">Infrastructure Overview</h1>
			<p class="text-sm text-muted-foreground">
				Unified declarative YAML inventory & live multi-collector network telemetry.
			</p>
		</div>
	</div>

	<!-- KPI Row -->
	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
		<a href={withAtParam('/devices', data.at)} class="block">
			<Card class="hover:border-primary/50 transition-colors">
				<CardHeader class="flex-row items-center justify-between pb-2">
					<CardTitle class="text-xs font-medium text-muted-foreground">Devices</CardTitle>
					<Server class="h-4 w-4 text-sky-500" />
				</CardHeader>
				<CardContent>
					<div class="text-2xl font-bold font-mono">{counts.devices}</div>
					<p class="text-xs text-muted-foreground mt-1 flex items-center gap-1">
						<Sparkles class="h-3 w-3 text-purple-500" />
						{counts.auto_devices} auto-discovered
					</p>
				</CardContent>
			</Card>
		</a>

		<a href={withAtParam('/cabling', data.at)} class="block">
			<Card class="hover:border-primary/50 transition-colors">
				<CardHeader class="flex-row items-center justify-between pb-2">
					<CardTitle class="text-xs font-medium text-muted-foreground">Cables & Links</CardTitle>
					<Cable class="h-4 w-4 text-emerald-500" />
				</CardHeader>
				<CardContent>
					<div class="text-2xl font-bold font-mono">{counts.cables}</div>
					<p class="text-xs text-muted-foreground mt-1">{counts.vlans} VLANs · {counts.prefixes} prefixes</p>
				</CardContent>
			</Card>
		</a>

		<a href={withAtParam('/racks', data.at)} class="block">
			<Card class="hover:border-primary/50 transition-colors">
				<CardHeader class="flex-row items-center justify-between pb-2">
					<CardTitle class="text-xs font-medium text-muted-foreground">Racks & Sites</CardTitle>
					<HardDrive class="h-4 w-4 text-indigo-500" />
				</CardHeader>
				<CardContent>
					<div class="text-2xl font-bold font-mono">{counts.racks}</div>
					<p class="text-xs text-muted-foreground mt-1">Across {counts.sites} site(s)</p>
				</CardContent>
			</Card>
		</a>

		<a href={withAtParam('/assets', data.at)} class="block">
			<Card class="hover:border-primary/50 transition-colors">
				<CardHeader class="flex-row items-center justify-between pb-2">
					<CardTitle class="text-xs font-medium text-muted-foreground">Tracked Assets</CardTitle>
					<Package class="h-4 w-4 text-amber-500" />
				</CardHeader>
				<CardContent>
					<div class="text-2xl font-bold font-mono">{counts.assets}</div>
					<p class="text-xs text-muted-foreground mt-1 flex items-center gap-1">
						<Users class="h-3 w-3" /> {counts.owners} owner(s)
					</p>
				</CardContent>
			</Card>
		</a>

		<a href={withAtParam('/system', data.at)} class="block">
			<Card class="hover:border-primary/50 transition-colors">
				<CardHeader class="flex-row items-center justify-between pb-2">
					<CardTitle class="text-xs font-medium text-muted-foreground">Drift & Issues</CardTitle>
					<AlertTriangle class={`h-4 w-4 ${counts.issues > 0 ? 'text-amber-500' : 'text-emerald-500'}`} />
				</CardHeader>
				<CardContent>
					<div class="text-2xl font-bold font-mono">{counts.issues}</div>
					<p class="text-xs text-muted-foreground mt-1">
						{(ov.issue_breakdown?.error ?? 0)} errors · {(ov.issue_breakdown?.warning ?? 0)} warnings
					</p>
				</CardContent>
			</Card>
		</a>
	</div>

	<div class="grid gap-6 lg:grid-cols-3">
		<!-- Collectors Status -->
		<Card class="lg:col-span-1">
			<CardHeader>
				<CardTitle class="flex items-center gap-2">
					<Activity class="h-4 w-4 text-primary" />
					Remote Collectors
				</CardTitle>
				<CardDescription>Outbound WebSocket collectors feeding observations</CardDescription>
			</CardHeader>
			<CardContent class="space-y-2.5">
				{#if (ov.collectors ?? []).length === 0}
					<div class="rounded-lg border border-dashed p-4 text-center text-xs text-muted-foreground">
						No collectors connected yet. Start <code class="font-mono">auto-netviz-collector</code> to stream live topology.
					</div>
				{:else}
					{#each ov.collectors ?? [] as col}
						<div class="rounded-lg border bg-muted/30 p-3 space-y-1.5">
							<div class="flex items-center justify-between">
								<div class="flex items-center gap-2">
									<span class={`h-2 w-2 rounded-full ${statusColorClass(col.status)}`}></span>
									<span class="text-sm font-medium">{col.name}</span>
								</div>
								<Badge variant={col.status === 'online' ? 'success' : 'destructive'}>{col.status}</Badge>
							</div>
							<div class="flex items-center justify-between text-[11px] font-mono text-muted-foreground">
								<span>site: {col.site || 'all'}</span>
								<span>seen {formatRelative(col.last_seen_at)}</span>
							</div>
							{#if col.sources && col.sources.length > 0}
								<div class="flex flex-wrap gap-1 pt-1">
									{#each col.sources as s}
										<span class="rounded border bg-background px-1.5 py-0.5 font-mono text-[10px]">
											{s.source}:{s.device_count}
										</span>
									{/each}
								</div>
							{/if}
						</div>
					{/each}
				{/if}

				<!-- Role breakdown -->
				<div class="pt-3 border-t">
					<div class="text-xs font-medium text-muted-foreground mb-2">Device Roles</div>
					<div class="flex flex-wrap gap-1.5">
						{#each Object.entries(ov.role_breakdown ?? {}) as [role, count]}
							<a href={withAtParam(`/devices?role=${encodeURIComponent(role)}`, data.at)}>
								<Badge variant="outline" class="font-mono hover:bg-accent">{role}: {count}</Badge>
							</a>
						{/each}
					</div>
				</div>
			</CardContent>
		</Card>

		<!-- Time Machine Activity Feed -->
		<Card class="lg:col-span-2">
			<CardHeader class="flex-row items-center justify-between">
				<div>
					<CardTitle>Recent Time Machine Events</CardTitle>
					<CardDescription>Audited additions, cabling updates, status flips & YAML reloads</CardDescription>
				</div>
				<a href={withAtParam('/timemachine', data.at)} class="text-xs font-medium text-primary hover:underline">
					Explore Full History →
				</a>
			</CardHeader>
			<CardContent>
				{#if (ov.recent_events ?? []).length === 0}
					<div class="p-6 text-center text-xs text-muted-foreground">No events recorded yet.</div>
				{:else}
					<div class="divide-y">
						{#each ov.recent_events ?? [] as ev}
							<div class="flex items-center justify-between py-2.5 gap-3">
								<div class="min-w-0">
									<div class="flex items-center gap-2">
										<span class="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
											{ev.event_type}
										</span>
										<a
											href={withAtParam(
												ev.entity_kind === 'device' ? `/devices/${encodeURIComponent(ev.entity_id)}` : `/timemachine?to=${ev.snapshot_id}`,
												String(ev.snapshot_id)
											)}
											class="text-sm font-medium truncate hover:underline"
										>
											{ev.summary}
										</a>
									</div>
								</div>
								<div class="shrink-0 text-right font-mono text-[11px] text-muted-foreground">
									<a href={withAtParam('/', String(ev.snapshot_id))} class="hover:text-primary">#{ev.snapshot_id}</a>
									<span class="ml-2">{formatRelative(ev.occurred_at)}</span>
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</CardContent>
		</Card>
	</div>
</div>
