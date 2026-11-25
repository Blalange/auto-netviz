<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import TopologyGraph from '$lib/components/graph/TopologyGraph.svelte';
	import Input from '$lib/components/ui/input/Input.svelte';
	import Button from '$lib/components/ui/button/Button.svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	let search = $state('');

	function updateFilter(key: string, val: string) {
		const url = new URL(page.url);
		if (val) url.searchParams.set(key, val);
		else url.searchParams.delete(key);
		goto(url.pathname + url.search, { keepFocus: true, noScroll: true });
	}

	let decoratedNodes = $derived.by(() => {
		const diffMap = new Map<string, 'added' | 'removed' | 'modified'>();
		if (data.diff) {
			for (const e of data.diff.entities ?? []) {
				if (e.entity_kind === 'device') {
					diffMap.set(e.entity_id, e.change);
				}
			}
		}
		return (data.nodes ?? []).map((n) => ({
			...n,
			diffState: diffMap.get(n.id) ?? null
		}));
	});
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<h1 class="text-xl font-bold tracking-tight">Network & Physical Topology</h1>
			<p class="text-xs text-muted-foreground">
				Interactive multi-layer graph ({data.nodes.length} devices, {data.edges.length} links)
			</p>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			<Input bind:value={search} placeholder="Highlight node or IP..." class="h-8 w-44 text-xs" />

			<select
				value={data.filters.site}
				onchange={(e) => updateFilter('site', (e.target as HTMLSelectElement).value)}
				class="h-8 rounded-md border bg-background px-2 text-xs"
			>
				<option value="">All sites</option>
				{#each data.sites as s}
					<option value={s.id}>{s.name}</option>
				{/each}
			</select>

			<select
				value={data.filters.layer}
				onchange={(e) => updateFilter('layer', (e.target as HTMLSelectElement).value)}
				class="h-8 rounded-md border bg-background px-2 text-xs"
			>
				<option value="all">All layers</option>
				<option value="physical">Physical + LLDP</option>
				<option value="l2">L2 Network</option>
			</select>

			<select
				value={data.filters.vlan}
				onchange={(e) => updateFilter('vlan', (e.target as HTMLSelectElement).value)}
				class="h-8 rounded-md border bg-background px-2 text-xs"
			>
				<option value="">All VLANs</option>
				{#each data.vlans as v}
					<option value={String(v.vid)}>VLAN {v.vid} · {v.name}</option>
				{/each}
			</select>

			<select
				value={data.filters.owner}
				onchange={(e) => updateFilter('owner', (e.target as HTMLSelectElement).value)}
				class="h-8 rounded-md border bg-background px-2 text-xs"
			>
				<option value="">All owners</option>
				{#each data.owners as o}
					<option value={o.name}>{o.name}</option>
				{/each}
			</select>

			{#if data.diffFrom}
				<Button variant="outline" size="sm" class="h-8 text-xs" onclick={() => updateFilter('diff_from', '')}>
					Clear Diff Overlay (#{data.diffFrom})
				</Button>
			{/if}
		</div>
	</div>

	<div class="flex flex-wrap items-center gap-4 text-[11px] text-muted-foreground">
		<span class="flex items-center gap-1.5"><span class="h-0.5 w-4 bg-sky-400 inline-block"></span> Manual cable</span>
		<span class="flex items-center gap-1.5"><span class="h-0.5 w-4 bg-emerald-500 inline-block"></span> Verified LLDP</span>
		<span class="flex items-center gap-1.5"><span class="h-0.5 w-4 bg-slate-500 inline-block"></span> Observed UniFi/LLDP</span>
		<span class="flex items-center gap-1.5"><span class="h-0.5 w-4 border-t-2 border-dashed border-purple-500 inline-block"></span> Inferred FDB</span>
	</div>

	<TopologyGraph rawNodes={decoratedNodes} rawEdges={data.edges} at={data.at} highlightQuery={search} />
</div>
