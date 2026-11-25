<script lang="ts">
	import RackElevation from '$lib/components/svg/RackElevation.svelte';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import Badge from '$lib/components/ui/badge/Badge.svelte';
	import OriginBadge from '$lib/components/shared/OriginBadge.svelte';
	import { statusColorClass, withAtParam } from '$lib/utils';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<div class="text-xs text-muted-foreground mb-1">
				<a href={withAtParam('/racks', data.at)} class="hover:underline">Racks</a> / {data.rack.site}
			</div>
			<h1 class="text-2xl font-bold tracking-tight">{data.rack.name}</h1>
			{#if data.rack.description}
				<p class="text-sm text-muted-foreground mt-0.5">{data.rack.description}</p>
			{/if}
		</div>
		<div class="flex items-center gap-2 font-mono text-xs">
			<Badge variant="outline">{data.rack.u_height}U · {data.rack.width}"</Badge>
			<Badge variant="outline">{data.rack.used_u}U occupied</Badge>
			{#if data.rack.power_budget_w}
				<Badge variant="outline">{data.rack.power_budget_w}W budget</Badge>
			{/if}
		</div>
	</div>

	<RackElevation
		rack={data.rack}
		devices={data.devices ?? []}
		deviceTypes={data.device_types ?? {}}
		cables={data.cables ?? []}
		at={data.at}
	/>

	{#if (data.unracked_devices ?? []).length > 0}
		<Card>
			<CardHeader>
				<CardTitle>Unracked Site Devices ({data.unracked_devices.length})</CardTitle>
				<CardDescription>Access points, workstations, and auto-discovered hosts at site "{data.rack.site}"</CardDescription>
			</CardHeader>
			<CardContent>
				<div class="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-4">
					{#each data.unracked_devices as d}
						<a
							href={withAtParam(`/devices/${encodeURIComponent(d.id)}`, data.at)}
							class="flex flex-col justify-between rounded-lg border bg-muted/30 p-3 hover:border-primary/50 transition-colors"
						>
							<div class="flex items-center justify-between gap-2">
								<span class="font-medium text-sm truncate">{d.name}</span>
								<span class={`h-2 w-2 rounded-full ${statusColorClass(d.status)}`}></span>
							</div>
							<div class="mt-2 flex items-center justify-between text-xs text-muted-foreground font-mono">
								<span>{d.role}</span>
								<span>{d.primary_ip || 'no IP'}</span>
							</div>
							<div class="mt-2">
								<OriginBadge origin={d.origin} />
							</div>
						</a>
					{/each}
				</div>
			</CardContent>
		</Card>
	{/if}
</div>
