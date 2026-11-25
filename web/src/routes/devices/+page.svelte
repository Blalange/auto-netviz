<script lang="ts">
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import Input from '$lib/components/ui/input/Input.svelte';
	import OriginBadge from '$lib/components/shared/OriginBadge.svelte';
	import ProvenanceBadges from '$lib/components/shared/ProvenanceBadges.svelte';
	import { statusColorClass, withAtParam } from '$lib/utils';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	let search = $state('');
	let roleFilter = $state('');
	let originFilter = $state('');

	$effect(() => {
		roleFilter = data.initialRole || '';
	});

	let roles = $derived(Array.from(new Set((data.devices ?? []).map((d) => d.role))).sort());
	let filtered = $derived(
		(data.devices ?? []).filter((d) => {
			if (roleFilter && d.role !== roleFilter) return false;
			if (originFilter && d.origin !== originFilter) return false;
			if (!search.trim()) return true;
			const q = search.toLowerCase();
			return (
				d.name.toLowerCase().includes(q) ||
				(d.primary_ip || '').toLowerCase().includes(q) ||
				(d.oob_ip || '').toLowerCase().includes(q) ||
				(d.serial || '').toLowerCase().includes(q) ||
				(d.owner || '').toLowerCase().includes(q) ||
				(d.manufacturer || '').toLowerCase().includes(q)
			);
		})
	);
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-bold tracking-tight">Devices</h1>
		<p class="text-sm text-muted-foreground">
			All manual, merged, and auto-discovered network & compute hardware.
		</p>
	</div>

	<Card>
		<CardHeader class="flex-row flex-wrap items-center justify-between gap-3">
			<CardTitle>Inventory ({filtered.length})</CardTitle>
			<div class="flex flex-wrap items-center gap-2">
				<Input bind:value={search} placeholder="Search name, IP, serial, owner..." class="h-8 w-56 text-xs" />
				<select bind:value={roleFilter} class="h-8 rounded-md border bg-background px-2 text-xs">
					<option value="">All roles</option>
					{#each roles as r}
						<option value={r}>{r}</option>
					{/each}
				</select>
				<select bind:value={originFilter} class="h-8 rounded-md border bg-background px-2 text-xs">
					<option value="">All origins</option>
					<option value="merged">Manual + Live</option>
					<option value="manual">Manual Only</option>
					<option value="auto">Auto-Discovered</option>
				</select>
			</div>
		</CardHeader>
		<CardContent>
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Device</TableHead>
						<TableHead>Role & Origin</TableHead>
						<TableHead>Hardware</TableHead>
						<TableHead>IPs</TableHead>
						<TableHead>Rack / Site</TableHead>
						<TableHead>Owner</TableHead>
						<TableHead>Sources</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{#each filtered as d}
						<TableRow>
							<TableCell>
								<a href={withAtParam(`/devices/${encodeURIComponent(d.id)}`, data.at)} class="flex items-center gap-2 font-medium hover:underline text-primary">
									<span class={`h-2 w-2 rounded-full ${statusColorClass(d.status)}`}></span>
									{d.name}
								</a>
								{#if d.serial}
									<div class="font-mono text-[10px] text-muted-foreground">S/N: {d.serial}</div>
								{/if}
							</TableCell>
							<TableCell>
								<div class="flex flex-col gap-1 items-start">
									<span class="font-mono text-xs">{d.role}</span>
									<OriginBadge origin={d.origin} />
								</div>
							</TableCell>
							<TableCell class="text-xs">
								<div>{d.manufacturer || '—'} {d.model || ''}</div>
								<div class="font-mono text-[10px] text-muted-foreground">{d.device_type}</div>
							</TableCell>
							<TableCell class="font-mono text-xs">
								{#if d.primary_ip}
									<div>{d.primary_ip}</div>
								{/if}
								{#if d.oob_ip}
									<div class="text-[10px] text-muted-foreground">OOB: {d.oob_ip}</div>
								{/if}
							</TableCell>
							<TableCell class="text-xs">
								<div>{d.site}</div>
								{#if d.rack}
									<div class="font-mono text-[10px] text-muted-foreground">{d.rack} · U{d.position}</div>
								{/if}
							</TableCell>
							<TableCell class="text-xs">{d.owner || '—'}</TableCell>
							<TableCell>
								<ProvenanceBadges sources={d.sources} />
							</TableCell>
						</TableRow>
					{/each}
				</TableBody>
			</Table>
		</CardContent>
	</Card>
</div>
