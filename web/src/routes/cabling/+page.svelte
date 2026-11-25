<script lang="ts">
	import { apiGet } from '$lib/api/client';
	import type { Cable } from '$lib/api/types';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import Badge from '$lib/components/ui/badge/Badge.svelte';
	import Button from '$lib/components/ui/button/Button.svelte';
	import Input from '$lib/components/ui/input/Input.svelte';
	import ProvenanceBadges from '$lib/components/shared/ProvenanceBadges.svelte';
	import { withAtParam } from '$lib/utils';
	import { Route } from '@lucide/svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	let filter = $state('');
	let confFilter = $state('');
	let traceFrom = $state('');
	let traceTo = $state('');
	let traceResult = $state<{ found: boolean; hops: Cable[] } | null>(null);
	let traceError = $state('');

	let filteredCables = $derived(
		(data.cables ?? []).filter((c) => {
			if (confFilter && c.confidence !== confFilter) return false;
			if (!filter.trim()) return true;
			const q = filter.toLowerCase();
			return (
				c.a_device.toLowerCase().includes(q) ||
				c.b_device.toLowerCase().includes(q) ||
				c.a_port.toLowerCase().includes(q) ||
				c.b_port.toLowerCase().includes(q) ||
				c.type.toLowerCase().includes(q)
			);
		})
	);

	async function runTrace() {
		traceError = '';
		traceResult = null;
		if (!traceFrom || !traceTo) return;
		try {
			traceResult = await apiGet<{ found: boolean; hops: Cable[] }>(
				`/api/v1/cables/trace?from=${encodeURIComponent(traceFrom)}&to=${encodeURIComponent(traceTo)}`,
				{ at: data.at }
			);
		} catch (e) {
			traceError = e instanceof Error ? e.message : 'Trace failed';
		}
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-bold tracking-tight">Cabling & Path Tracer</h1>
		<p class="text-sm text-muted-foreground">
			Physical cables declared in YAML merged with LLDP, CDP, UniFi, and SNMP FDB links.
		</p>
	</div>

	<!-- Path Tracer -->
	<Card>
		<CardHeader>
			<CardTitle class="flex items-center gap-2">
				<Route class="h-4 w-4 text-primary" />
				Shortest-Path Cable Tracer
			</CardTitle>
			<CardDescription>Find the physical/L2 hop path between any two devices</CardDescription>
		</CardHeader>
		<CardContent class="space-y-4">
			<div class="flex flex-wrap items-center gap-3">
				<select bind:value={traceFrom} class="h-9 rounded-md border bg-background px-3 text-sm min-w-[180px]">
					<option value="">Source device...</option>
					{#each data.devices as d}
						<option value={d.id}>{d.name}</option>
					{/each}
				</select>
				<span class="text-muted-foreground font-mono">→</span>
				<select bind:value={traceTo} class="h-9 rounded-md border bg-background px-3 text-sm min-w-[180px]">
					<option value="">Destination device...</option>
					{#each data.devices as d}
						<option value={d.id}>{d.name}</option>
					{/each}
				</select>
				<Button size="sm" onclick={runTrace} disabled={!traceFrom || !traceTo}>Trace Path</Button>
			</div>

			{#if traceError}
				<p class="text-xs text-destructive">{traceError}</p>
			{:else if traceResult}
				{#if !traceResult.found}
					<p class="text-xs text-amber-500">No continuous physical/L2 cable path found between those devices.</p>
				{:else if (traceResult.hops ?? []).length === 0}
					<p class="text-xs text-muted-foreground">Source and destination are the same device (0 hops).</p>
				{:else}
					<div class="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/40 p-3">
						{#each traceResult.hops as hop, idx}
							<div class="rounded border bg-card px-2.5 py-1.5 font-mono text-xs">
								<span class="font-semibold">{hop.a_device}</span>:{hop.a_port} ↔ <span class="font-semibold">{hop.b_device}</span>:{hop.b_port}
								<span class="ml-1 text-[10px] text-muted-foreground">({hop.type})</span>
							</div>
							{#if idx < traceResult.hops.length - 1}
								<span class="text-primary font-bold">→</span>
							{/if}
						{/each}
					</div>
				{/if}
			{/if}
		</CardContent>
	</Card>

	<!-- Cable Table -->
	<Card>
		<CardHeader class="flex-row flex-wrap items-center justify-between gap-3">
			<div>
				<CardTitle>All Cables ({filteredCables.length})</CardTitle>
			</div>
			<div class="flex items-center gap-2">
				<Input bind:value={filter} placeholder="Filter device, port, type..." class="h-8 w-56 text-xs" />
				<select bind:value={confFilter} class="h-8 rounded-md border bg-background px-2 text-xs">
					<option value="">All confidences</option>
					<option value="manual">Manual</option>
					<option value="verified">Verified (Mutual LLDP)</option>
					<option value="observed">Observed</option>
					<option value="inferred">Inferred (FDB)</option>
				</select>
			</div>
		</CardHeader>
		<CardContent>
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Side A</TableHead>
						<TableHead>Side B</TableHead>
						<TableHead>Media</TableHead>
						<TableHead>Confidence</TableHead>
						<TableHead>Sources</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{#each filteredCables as c}
						<TableRow>
							<TableCell class="font-mono text-xs">
								<a href={withAtParam(`/devices/${encodeURIComponent(c.a_device_id)}`, data.at)} class="font-semibold hover:underline text-primary">
									{c.a_device}
								</a>
								<span class="text-muted-foreground"> : {c.a_port}</span>
							</TableCell>
							<TableCell class="font-mono text-xs">
								<a href={withAtParam(`/devices/${encodeURIComponent(c.b_device_id)}`, data.at)} class="font-semibold hover:underline text-primary">
									{c.b_device}
								</a>
								<span class="text-muted-foreground"> : {c.b_port}</span>
							</TableCell>
							<TableCell class="font-mono text-xs">
								{c.type}{#if c.speed_mbps} · {c.speed_mbps}M{/if}
							</TableCell>
							<TableCell>
								<Badge variant={c.confidence === 'verified' ? 'success' : c.confidence === 'inferred' ? 'warning' : 'outline'}>
									{c.confidence}
								</Badge>
							</TableCell>
							<TableCell>
								<ProvenanceBadges sources={c.sources} />
							</TableCell>
						</TableRow>
					{/each}
				</TableBody>
			</Table>
		</CardContent>
	</Card>
</div>
