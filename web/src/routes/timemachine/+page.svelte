<script lang="ts">
	import { goto } from '$app/navigation';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import Badge from '$lib/components/ui/badge/Badge.svelte';
	import Button from '$lib/components/ui/button/Button.svelte';
	import { formatTime, withAtParam } from '$lib/utils';
	import { GitCompare, History, Share2 } from '@lucide/svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	let fromSel = $state('');
	let toSel = $state('');

	$effect(() => {
		fromSel = data.fromParam;
		toSel = data.toParam;
	});

	function compare() {
		goto(`/timemachine?from=${encodeURIComponent(fromSel)}&to=${encodeURIComponent(toSel)}`, { keepFocus: true });
	}
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight flex items-center gap-2">
				<History class="h-6 w-6 text-primary" />
				Time Machine & Snapshot Diff
			</h1>
			<p class="text-sm text-muted-foreground">
				Content-addressed point-in-time snapshots. Compare any two moments or jump into historical views.
			</p>
		</div>
	</div>

	<!-- Snapshot Diff Controls -->
	<Card>
		<CardHeader>
			<CardTitle class="flex items-center gap-2">
				<GitCompare class="h-4 w-4 text-primary" />
				Compare Snapshots
			</CardTitle>
			<CardDescription>Inspect added, removed, and modified entities between any two snapshots</CardDescription>
		</CardHeader>
		<CardContent class="space-y-4">
			<div class="flex flex-wrap items-center gap-3">
				<select bind:value={fromSel} class="h-9 rounded-md border bg-background px-3 text-xs font-mono">
					{#each data.snapshots as s}
						<option value={String(s.id)}>#{s.id} · {formatTime(s.created_at)} ({s.trigger})</option>
					{/each}
				</select>
				<span class="font-mono text-muted-foreground">→</span>
				<select bind:value={toSel} class="h-9 rounded-md border bg-background px-3 text-xs font-mono">
					{#each data.snapshots as s}
						<option value={String(s.id)}>#{s.id} · {formatTime(s.created_at)} ({s.trigger})</option>
					{/each}
				</select>
				<Button size="sm" onclick={compare}>Compare</Button>
				{#if fromSel && toSel}
					<Button
						variant="outline"
						size="sm"
						href={`/topology?at=${encodeURIComponent(toSel)}&diff_from=${encodeURIComponent(fromSel)}`}
					>
						<Share2 class="h-3.5 w-3.5" />
						Visual Topology Diff
					</Button>
				{/if}
			</div>

			{#if data.diff}
				<div class="flex flex-wrap gap-2 pt-2">
					<Badge variant="success">+{data.diff.added_count} Added</Badge>
					<Badge variant="destructive">-{data.diff.removed_count} Removed</Badge>
					<Badge variant="warning">~{data.diff.modified_count} Modified</Badge>
				</div>

				{#if (data.diff.entities ?? []).length === 0}
					<div class="rounded-lg border border-dashed p-6 text-center text-xs text-muted-foreground">
						No entity differences between snapshot #{data.diff.from_snapshot.id} and #{data.diff.to_snapshot.id}.
					</div>
				{:else}
					<div class="space-y-2.5">
						{#each data.diff.entities ?? [] as ent}
							<div
								class={`rounded-lg border p-3 ${
									ent.change === 'added'
										? 'border-emerald-500/40 bg-emerald-500/5'
										: ent.change === 'removed'
											? 'border-rose-500/40 bg-rose-500/5'
											: 'border-amber-500/40 bg-amber-500/5'
								}`}
							>
								<div class="flex items-center justify-between gap-2">
									<div class="flex items-center gap-2">
										<Badge variant={ent.change === 'added' ? 'success' : ent.change === 'removed' ? 'destructive' : 'warning'}>
											{ent.change}
										</Badge>
										<span class="font-mono text-xs text-muted-foreground">{ent.entity_kind}</span>
										<span class="font-semibold text-sm">{ent.name || ent.entity_id}</span>
									</div>
								</div>

								{#if ent.fields && ent.fields.length > 0}
									<div class="mt-2 space-y-1 font-mono text-xs">
										{#each ent.fields as f}
											<div class="rounded bg-background/70 px-2.5 py-1.5 border">
												<span class="font-semibold text-foreground">{f.field}:</span>
												<span class="text-rose-600 dark:text-rose-400 line-through mx-1">{JSON.stringify(f.old)}</span>
												<span class="text-muted-foreground">→</span>
												<span class="text-emerald-600 dark:text-emerald-400 ml-1">{JSON.stringify(f.new)}</span>
											</div>
										{/each}
									</div>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			{/if}
		</CardContent>
	</Card>

	<!-- Snapshot History Table -->
	<Card>
		<CardHeader>
			<CardTitle>Recorded Snapshots ({data.snapshots.length})</CardTitle>
		</CardHeader>
		<CardContent>
			<div class="divide-y">
				{#each data.snapshots as s}
					<div class="flex flex-wrap items-center justify-between py-2.5 gap-3 text-xs">
						<div class="flex items-center gap-3">
							<span class="font-mono font-bold text-primary">#{s.id}</span>
							<span>{formatTime(s.created_at)}</span>
							<Badge variant="outline" class="font-mono">{s.trigger}</Badge>
							<span class="text-muted-foreground">{s.trigger_detail}</span>
						</div>
						<div class="flex items-center gap-3 font-mono">
							<span class="text-muted-foreground">{s.counts.devices} dev · {s.counts.cables} cables</span>
							<a href={withAtParam('/', String(s.id))} class="text-primary hover:underline">Browse Snapshot →</a>
						</div>
					</div>
				{/each}
			</div>
		</CardContent>
	</Card>
</div>
