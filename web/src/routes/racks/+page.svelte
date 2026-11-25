<script lang="ts">
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import Badge from '$lib/components/ui/badge/Badge.svelte';
	import { withAtParam } from '$lib/utils';
	import { HardDrive } from '@lucide/svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-bold tracking-tight">Racks & Physical Elevations</h1>
		<p class="text-sm text-muted-foreground">
			Select a rack to inspect front & rear parametric SVG elevations and interactive port cabling.
		</p>
	</div>

	{#if data.racks.length === 0}
		<Card>
			<CardContent class="p-8 text-center text-sm text-muted-foreground">
				No racks defined in manual YAML. Add a <code class="font-mono">racks</code> block to visualize rack elevations.
			</CardContent>
		</Card>
	{:else}
		<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
			{#each data.racks as rack}
				{@const util = rack.u_height > 0 ? Math.round((rack.used_u / rack.u_height) * 100) : 0}
				<a href={withAtParam(`/racks/${encodeURIComponent(rack.id)}`, data.at)} class="block">
					<Card class="hover:border-primary/60 transition-colors h-full">
						<CardHeader class="flex-row items-start justify-between">
							<div>
								<CardTitle class="flex items-center gap-2 text-base">
									<HardDrive class="h-4 w-4 text-primary" />
									{rack.name}
								</CardTitle>
								<CardDescription class="mt-1">
									Site: {rack.site}{rack.location ? ` · ${rack.location}` : ''}
								</CardDescription>
							</div>
							<Badge variant="outline" class="font-mono">{rack.u_height}U</Badge>
						</CardHeader>
						<CardContent class="space-y-3">
							<div>
								<div class="mb-1 flex justify-between text-xs font-mono">
									<span>Space Used</span>
									<span>{rack.used_u} / {rack.u_height}U ({util}%)</span>
								</div>
								<div class="h-2 w-full rounded-full bg-muted overflow-hidden">
									<div class="h-full bg-primary transition-all" style={`width: ${Math.min(100, util)}%`}></div>
								</div>
							</div>
							<div class="flex items-center justify-between text-xs text-muted-foreground">
								<span>{(rack.slots ?? []).length} mounted devices</span>
								{#if rack.power_budget_w}
									<span class="font-mono">{rack.power_budget_w}W budget</span>
								{/if}
							</div>
						</CardContent>
					</Card>
				</a>
			{/each}
		</div>
	{/if}
</div>
