<script lang="ts">
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import Badge from '$lib/components/ui/badge/Badge.svelte';
	import { formatRelative, statusColorClass, withAtParam } from '$lib/utils';
	import { Activity, AlertTriangle } from '@lucide/svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-bold tracking-tight">System, Collectors & Diagnostics</h1>
		<p class="text-sm text-muted-foreground">
			Remote collector WebSocket health, per-source schedules, YAML validation errors, merge conflicts, and live drift alerts.
		</p>
	</div>

	<Card>
		<CardHeader>
			<CardTitle class="flex items-center gap-2">
				<Activity class="h-4 w-4 text-primary" />
				Collectors ({data.collectors.length})
			</CardTitle>
			<CardDescription>Each collector connects outbound over WebSocket and holds its own local credentials</CardDescription>
		</CardHeader>
		<CardContent class="space-y-4">
			{#if data.collectors.length === 0}
				<p class="text-xs text-muted-foreground">No collectors have registered yet.</p>
			{:else}
				{#each data.collectors as col}
					<div class="rounded-xl border p-4 space-y-3">
						<div class="flex flex-wrap items-center justify-between gap-2">
							<div class="flex items-center gap-2.5">
								<span class={`h-2.5 w-2.5 rounded-full ${statusColorClass(col.status)}`}></span>
								<span class="font-semibold text-sm">{col.name}</span>
								<span class="font-mono text-xs text-muted-foreground">({col.collector_id})</span>
							</div>
							<div class="flex items-center gap-2 font-mono text-xs">
								<Badge variant="outline">site: {col.site || 'all'}</Badge>
								{#if col.zone}<Badge variant="outline">zone: {col.zone}</Badge>{/if}
								<Badge variant={col.status === 'online' ? 'success' : 'destructive'}>{col.status}</Badge>
								<span class="text-muted-foreground">seen {formatRelative(col.last_seen_at)}</span>
							</div>
						</div>

						{#if col.sources && col.sources.length > 0}
							<div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
								{#each col.sources as src}
									<div class="rounded-lg border bg-muted/30 p-2.5 text-xs space-y-1">
										<div class="flex items-center justify-between">
											<span class="font-mono font-semibold">{src.source}:{src.profile}</span>
											<Badge variant={src.status === 'ok' ? 'success' : 'destructive'}>{src.status}</Badge>
										</div>
										<div class="font-mono text-[11px] text-muted-foreground">
											{src.device_count} devs · {src.link_count} links · {src.duration_ms}ms
										</div>
										{#if src.error}
											<div class="text-[11px] text-destructive break-words">{src.error}</div>
										{/if}
									</div>
								{/each}
							</div>
						{/if}
					</div>
				{/each}
			{/if}
		</CardContent>
	</Card>

	<Card>
		<CardHeader>
			<CardTitle class="flex items-center gap-2">
				<AlertTriangle class="h-4 w-4 text-amber-500" />
				Validation, Drift & Merge Issues ({data.issues.length})
			</CardTitle>
		</CardHeader>
		<CardContent>
			{#if data.issues.length === 0}
				<div class="rounded-lg border border-dashed p-6 text-center text-xs text-muted-foreground">
					Clean state! Zero YAML errors, merge conflicts, or manual-vs-live drift detected.
				</div>
			{:else}
				<div class="space-y-2">
					{#each data.issues as iss}
						<div class="rounded-lg border p-3 flex flex-wrap items-center justify-between gap-2 text-xs">
							<div class="space-y-0.5">
								<div class="flex items-center gap-2">
									<Badge variant={iss.severity === 'error' ? 'destructive' : iss.severity === 'warning' ? 'warning' : 'outline'}>
										{iss.severity}
									</Badge>
									<span class="font-mono text-muted-foreground">{iss.kind}</span>
									{#if iss.entity_id && iss.entity_kind === 'device'}
										<a href={withAtParam(`/devices/${encodeURIComponent(iss.entity_id)}`, data.at)} class="font-semibold text-primary hover:underline">
											{iss.entity_name}
										</a>
									{/if}
								</div>
								<div>{iss.message}</div>
							</div>
							{#if iss.file_path}
								<span class="font-mono text-[10px] text-muted-foreground">{iss.file_path}</span>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</CardContent>
	</Card>
</div>
