<script lang="ts">
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import Badge from '$lib/components/ui/badge/Badge.svelte';
	import Input from '$lib/components/ui/input/Input.svelte';
	import ProvenanceBadges from '$lib/components/shared/ProvenanceBadges.svelte';
	import { withAtParam } from '$lib/utils';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	let search = $state('');
	let kindFilter = $state('');

	let filtered = $derived(
		(data.assets ?? []).filter((a) => {
			if (kindFilter && a.kind !== kindFilter) return false;
			if (!search.trim()) return true;
			const q = search.toLowerCase();
			return (
				a.name.toLowerCase().includes(q) ||
				(a.asset_tag || '').toLowerCase().includes(q) ||
				(a.serial || '').toLowerCase().includes(q) ||
				(a.owner || '').toLowerCase().includes(q) ||
				(a.model || '').toLowerCase().includes(q)
			);
		})
	);
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-bold tracking-tight">Asset Inventory</h1>
		<p class="text-sm text-muted-foreground">
			Unified tracking across standalone spares/tools, racked devices, and serialized sub-components (drives, DIMMs, PSUs).
		</p>
	</div>

	<Card>
		<CardHeader class="flex-row flex-wrap items-center justify-between gap-3">
			<CardTitle>Tracked Assets ({filtered.length})</CardTitle>
			<div class="flex items-center gap-2">
				<Input bind:value={search} placeholder="Filter tag, serial, model, owner..." class="h-8 w-56 text-xs" />
				<select bind:value={kindFilter} class="h-8 rounded-md border bg-background px-2 text-xs">
					<option value="">All kinds</option>
					<option value="standalone">Standalone / Spares</option>
					<option value="device">Devices</option>
					<option value="module">Serialized Modules</option>
				</select>
			</div>
		</CardHeader>
		<CardContent>
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Asset Tag / Name</TableHead>
						<TableHead>Kind & Category</TableHead>
						<TableHead>Manufacturer & Model</TableHead>
						<TableHead>Serial</TableHead>
						<TableHead>Location / Host</TableHead>
						<TableHead>Owner & Warranty</TableHead>
						<TableHead>Sources</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{#each filtered as a}
						<TableRow>
							<TableCell>
								<div class="font-medium text-xs">{a.name}</div>
								{#if a.asset_tag}
									<div class="font-mono text-[10px] text-primary">{a.asset_tag}</div>
								{/if}
							</TableCell>
							<TableCell>
								<Badge variant="outline" class="font-mono">{a.kind}</Badge>
								{#if a.category}
									<div class="text-[11px] text-muted-foreground mt-0.5">{a.category}</div>
								{/if}
							</TableCell>
							<TableCell class="text-xs">{a.manufacturer || '—'} {a.model || ''}</TableCell>
							<TableCell class="font-mono text-xs">{a.serial || '—'}</TableCell>
							<TableCell class="text-xs">
								<div>{a.site || '—'}{a.location ? ` / ${a.location}` : ''}</div>
								{#if a.device_id}
									<a href={withAtParam(`/devices/${encodeURIComponent(a.device_id)}`, data.at)} class="text-primary hover:underline text-[11px]">
										Host: {a.device_name}
									</a>
								{/if}
							</TableCell>
							<TableCell class="text-xs">
								<div>{a.owner || '—'}</div>
								{#if a.warranty_until}
									<div class="font-mono text-[10px] text-muted-foreground">Exp: {a.warranty_until}</div>
								{/if}
							</TableCell>
							<TableCell><ProvenanceBadges sources={a.sources} /></TableCell>
						</TableRow>
					{/each}
				</TableBody>
			</Table>
		</CardContent>
	</Card>
</div>
