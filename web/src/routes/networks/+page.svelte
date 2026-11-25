<script lang="ts">
	import { apiGet } from '$lib/api/client';
	import type { IPAddress, Prefix } from '$lib/api/types';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import Badge from '$lib/components/ui/badge/Badge.svelte';
	import ProvenanceBadges from '$lib/components/shared/ProvenanceBadges.svelte';
	import { withAtParam } from '$lib/utils';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	let selectedPrefix = $state<Prefix | null>(null);
	let prefixIPs = $state<IPAddress[]>([]);
	let loadingIPs = $state(false);

	async function openPrefix(pfx: Prefix) {
		if (selectedPrefix?.id === pfx.id) {
			selectedPrefix = null;
			prefixIPs = [];
			return;
		}
		selectedPrefix = pfx;
		loadingIPs = true;
		try {
			const res = await apiGet<{ ip_addresses: IPAddress[] }>(
				`/api/v1/networks/prefixes/${encodeURIComponent(pfx.id)}/ips`,
				{ at: data.at }
			);
			prefixIPs = res.ip_addresses ?? [];
		} catch {
			prefixIPs = [];
		} finally {
			loadingIPs = false;
		}
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-bold tracking-tight">Networks, VLANs & IPAM</h1>
		<p class="text-sm text-muted-foreground">
			Declared and auto-discovered subnets, VLANs, and live assigned IP addresses.
		</p>
	</div>

	<Card>
		<CardHeader>
			<CardTitle>Prefixes ({data.prefixes.length})</CardTitle>
			<CardDescription>Click any prefix row to inspect assigned IP addresses</CardDescription>
		</CardHeader>
		<CardContent class="space-y-4">
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Prefix</TableHead>
						<TableHead>Site</TableHead>
						<TableHead>VLAN</TableHead>
						<TableHead>Gateway</TableHead>
						<TableHead>Utilization</TableHead>
						<TableHead>Sources</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{#each data.prefixes as pfx}
						<TableRow class="cursor-pointer" onclick={() => openPrefix(pfx)}>
							<TableCell class="font-mono text-xs font-semibold text-primary">
								{pfx.cidr}
								{#if pfx.description}
									<div class="font-sans font-normal text-[11px] text-muted-foreground">{pfx.description}</div>
								{/if}
							</TableCell>
							<TableCell class="text-xs">{pfx.site}</TableCell>
							<TableCell class="font-mono text-xs">{pfx.vlan ? `VLAN ${pfx.vlan}` : '—'}</TableCell>
							<TableCell class="font-mono text-xs">{pfx.gateway || '—'}</TableCell>
							<TableCell class="w-48">
								<div class="flex items-center justify-between text-[11px] font-mono mb-1">
									<span>{pfx.used_ips} used</span>
									<span>{pfx.utilization.toFixed(1)}%</span>
								</div>
								<div class="h-1.5 w-full rounded-full bg-muted overflow-hidden">
									<div class="h-full bg-emerald-500" style={`width: ${Math.min(100, pfx.utilization)}%`}></div>
								</div>
							</TableCell>
							<TableCell><ProvenanceBadges sources={pfx.sources} /></TableCell>
						</TableRow>
					{/each}
				</TableBody>
			</Table>

			{#if selectedPrefix}
				<div class="rounded-lg border bg-muted/30 p-4">
					<h3 class="text-sm font-semibold font-mono mb-2">
						IP Addresses inside {selectedPrefix.cidr} ({prefixIPs.length})
					</h3>
					{#if loadingIPs}
						<p class="text-xs text-muted-foreground">Loading IP allocations...</p>
					{:else if prefixIPs.length === 0}
						<p class="text-xs text-muted-foreground">No device IPs recorded inside this prefix.</p>
					{:else}
						<div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
							{#each prefixIPs as ip}
								<div class="rounded border bg-card px-3 py-2 text-xs font-mono flex items-center justify-between">
									<div>
										<div class="font-semibold">{ip.address}</div>
										<div class="text-[11px] text-muted-foreground">
											{#if ip.device_id}
												<a href={withAtParam(`/devices/${encodeURIComponent(ip.device_id)}`, data.at)} class="text-primary hover:underline">
													{ip.device_name}
												</a>
												({ip.interface})
											{/if}
										</div>
									</div>
									{#if ip.dns_name}
										<Badge variant="outline">{ip.dns_name}</Badge>
									{/if}
								</div>
							{/each}
						</div>
					{/if}
				</div>
			{/if}
		</CardContent>
	</Card>

	<Card>
		<CardHeader>
			<CardTitle>VLAN Directory ({data.vlans.length})</CardTitle>
		</CardHeader>
		<CardContent>
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>VID</TableHead>
						<TableHead>Name</TableHead>
						<TableHead>Site</TableHead>
						<TableHead>Description</TableHead>
						<TableHead>Sources</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{#each data.vlans as v}
						<TableRow>
							<TableCell class="font-mono text-xs font-semibold">
								<span class="inline-flex items-center gap-2">
									{#if v.color}
										<span class="h-2.5 w-2.5 rounded-full" style={`background-color: ${v.color}`}></span>
									{/if}
									{v.vid}
								</span>
							</TableCell>
							<TableCell class="text-xs font-medium">{v.name}</TableCell>
							<TableCell class="text-xs">{v.site}</TableCell>
							<TableCell class="text-xs text-muted-foreground">{v.description || '—'}</TableCell>
							<TableCell><ProvenanceBadges sources={v.sources} /></TableCell>
						</TableRow>
					{/each}
				</TableBody>
			</Table>
		</CardContent>
	</Card>
</div>
