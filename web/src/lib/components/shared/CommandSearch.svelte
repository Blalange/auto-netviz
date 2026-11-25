<script lang="ts">
	import { goto } from '$app/navigation';
	import { apiGet } from '$lib/api/client';
	import type { SearchResult } from '$lib/api/types';
	import Dialog from '$lib/components/ui/dialog/Dialog.svelte';
	import Input from '$lib/components/ui/input/Input.svelte';
	import { withAtParam } from '$lib/utils';
	import { Search, Server, HardDrive, Cable, Network, Users, Box } from '@lucide/svelte';

	let { open = $bindable(false), at = null }: { open?: boolean; at?: string | null } = $props();
	let query = $state('');
	let results = $state<SearchResult[]>([]);
	let loading = $state(false);
	let timer: ReturnType<typeof setTimeout> | null = null;

	function onKey(e: KeyboardEvent) {
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
			e.preventDefault();
			open = !open;
		}
	}

	$effect(() => {
		if (!open) {
			query = '';
			results = [];
		}
	});

	$effect(() => {
		const q = query.trim();
		if (timer) clearTimeout(timer);
		if (!q) {
			results = [];
			return;
		}
		loading = true;
		timer = setTimeout(async () => {
			try {
				const res = await apiGet<{ results: SearchResult[] }>(`/api/v1/search?q=${encodeURIComponent(q)}`, { at });
				results = res.results ?? [];
			} catch {
				results = [];
			} finally {
				loading = false;
			}
		}, 160);
	});

	function targetHref(r: SearchResult): string {
		let path = '/';
		switch (r.entity_kind) {
			case 'device':
				path = `/devices/${encodeURIComponent(r.entity_id)}`;
				break;
			case 'rack':
				path = `/racks/${encodeURIComponent(r.entity_id)}`;
				break;
			case 'vlan':
			case 'prefix':
				path = '/networks';
				break;
			case 'cable':
				path = '/cabling';
				break;
			case 'asset':
				path = '/assets';
				break;
			case 'owner':
			case 'contact':
				path = '/ownership';
				break;
			default:
				path = '/devices';
		}
		return withAtParam(path, at);
	}

	function selectResult(r: SearchResult) {
		open = false;
		goto(targetHref(r));
	}
</script>

<svelte:window onkeydown={onKey} />

<Dialog bind:open title="Quick Search (Cmd+K)">
	<div class="relative mb-3">
		<Search class="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
		<Input
			bind:value={query}
			placeholder="Search devices, IPs, MACs, serials, asset tags, racks, VLANs, owners..."
			class="pl-9"
			autofocus
		/>
	</div>

	<div class="max-h-80 overflow-y-auto divide-y rounded-lg border">
		{#if !query.trim()}
			<div class="p-6 text-center text-xs text-muted-foreground">
				Type any hostname, IP address, MAC, serial number, asset tag, VLAN, or owner name.
			</div>
		{:else if loading}
			<div class="p-6 text-center text-xs text-muted-foreground">Searching...</div>
		{:else if results.length === 0}
			<div class="p-6 text-center text-xs text-muted-foreground">No matches found for "{query}".</div>
		{:else}
			{#each results as r}
				<button
					type="button"
					onclick={() => selectResult(r)}
					class="flex w-full items-center justify-between px-3 py-2.5 text-left hover:bg-accent transition-colors cursor-pointer"
				>
					<div class="flex items-center gap-2.5 min-w-0">
						{#if r.entity_kind === 'device'}
							<Server class="h-4 w-4 text-sky-500 shrink-0" />
						{:else if r.entity_kind === 'rack'}
							<HardDrive class="h-4 w-4 text-indigo-500 shrink-0" />
						{:else if r.entity_kind === 'cable'}
							<Cable class="h-4 w-4 text-amber-500 shrink-0" />
						{:else if r.entity_kind === 'vlan' || r.entity_kind === 'prefix'}
							<Network class="h-4 w-4 text-emerald-500 shrink-0" />
						{:else if r.entity_kind === 'owner' || r.entity_kind === 'contact'}
							<Users class="h-4 w-4 text-pink-500 shrink-0" />
						{:else}
							<Box class="h-4 w-4 text-purple-500 shrink-0" />
						{/if}
						<div class="truncate">
							<div class="text-sm font-medium truncate">{r.title}</div>
							{#if r.subtitle}
								<div class="text-xs text-muted-foreground truncate font-mono">{r.subtitle}</div>
							{/if}
						</div>
					</div>
					<span class="ml-2 shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] font-mono uppercase text-muted-foreground">
						{r.entity_kind}
					</span>
				</button>
			{/each}
		{/if}
	</div>
</Dialog>
