<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { SnapshotSummary } from '$lib/api/types';
	import Button from '$lib/components/ui/button/Button.svelte';
	import { formatTime } from '$lib/utils';
	import { History, ChevronLeft, ChevronRight, Radio, GitCompare } from '@lucide/svelte';

	let {
		snapshots = [],
		activeSnapshot = null,
		at = null
	}: {
		snapshots?: SnapshotSummary[];
		activeSnapshot?: SnapshotSummary | null;
		at?: string | null;
	} = $props();

	let isHistorical = $derived(Boolean(at && at !== 'live'));
	// snapshots arrive newest first [0 = latest ... N-1 = oldest]; reverse for left->right scrubber
	let chronological = $derived([...snapshots].reverse());
	let currentIndex = $derived.by(() => {
		if (chronological.length === 0) return -1;
		if (!activeSnapshot) return chronological.length - 1;
		const idx = chronological.findIndex((s) => s.id === activeSnapshot.id);
		return idx >= 0 ? idx : chronological.length - 1;
	});

	function navigateToAt(nextAt: string | null) {
		const url = new URL(page.url);
		if (nextAt) {
			url.searchParams.set('at', nextAt);
		} else {
			url.searchParams.delete('at');
		}
		goto(url.pathname + url.search, { keepFocus: true, noScroll: true });
	}

	function stepPrev() {
		if (currentIndex > 0) {
			navigateToAt(String(chronological[currentIndex - 1].id));
		}
	}

	function stepNext() {
		if (currentIndex >= 0 && currentIndex < chronological.length - 1) {
			const next = chronological[currentIndex + 1];
			if (currentIndex + 1 === chronological.length - 1) {
				navigateToAt(null);
			} else {
				navigateToAt(String(next.id));
			}
		}
	}

	function onScrub(e: Event) {
		const val = Number((e.target as HTMLInputElement).value);
		const snap = chronological[val];
		if (!snap) return;
		if (val === chronological.length - 1) {
			navigateToAt(null);
		} else {
			navigateToAt(String(snap.id));
		}
	}

	function diffLink(): string {
		if (!activeSnapshot || chronological.length < 2) return '/timemachine';
		const prevIdx = Math.max(0, currentIndex - 1);
		const fromId = chronological[prevIdx]?.id ?? activeSnapshot.id;
		return `/timemachine?from=${fromId}&to=${activeSnapshot.id}`;
	}
</script>

<div
	class={`sticky top-0 z-30 border-b px-4 py-2 transition-colors ${
		isHistorical
			? 'bg-amber-500/12 border-amber-500/40 backdrop-blur-md'
			: 'bg-background/85 backdrop-blur-md'
	}`}
>
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div class="flex items-center gap-2.5">
			{#if isHistorical}
				<span class="inline-flex items-center gap-1.5 rounded-full bg-amber-500 px-2.5 py-0.5 text-xs font-semibold text-amber-950">
					<History class="h-3.5 w-3.5" />
					TIME MACHINE
				</span>
			{:else}
				<span class="inline-flex items-center gap-1.5 rounded-full bg-emerald-500/15 border border-emerald-500/30 px-2.5 py-0.5 text-xs font-semibold text-emerald-700 dark:text-emerald-300">
					<Radio class="h-3 w-3 animate-pulse" />
					LIVE
				</span>
			{/if}

			{#if activeSnapshot}
				<div class="flex items-center gap-2 text-xs">
					<span class="font-mono font-semibold">Snapshot #{activeSnapshot.id}</span>
					<span class="text-muted-foreground">·</span>
					<span class="text-muted-foreground">{formatTime(activeSnapshot.created_at)}</span>
					<span class="hidden sm:inline rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
						{activeSnapshot.trigger}
					</span>
				</div>
			{:else}
				<span class="text-xs text-muted-foreground">Waiting for initial snapshot...</span>
			{/if}
		</div>

		{#if chronological.length > 0}
			<div class="flex flex-1 items-center gap-2 max-w-md min-w-[200px]">
				<Button variant="outline" size="icon" class="h-7 w-7 shrink-0" disabled={currentIndex <= 0} onclick={stepPrev} title="Previous snapshot">
					<ChevronLeft class="h-3.5 w-3.5" />
				</Button>
				<input
					type="range"
					min={0}
					max={Math.max(0, chronological.length - 1)}
					value={Math.max(0, currentIndex)}
					oninput={onScrub}
					class="h-1.5 w-full cursor-pointer accent-primary"
					aria-label="Time Machine Snapshot Scrubber"
				/>
				<Button
					variant="outline"
					size="icon"
					class="h-7 w-7 shrink-0"
					disabled={currentIndex < 0 || currentIndex >= chronological.length - 1}
					onclick={stepNext}
					title="Next snapshot"
				>
					<ChevronRight class="h-3.5 w-3.5" />
				</Button>
			</div>
		{/if}

		<div class="flex items-center gap-2">
			<Button variant="outline" size="sm" class="h-7 text-xs" href={diffLink()}>
				<GitCompare class="h-3.5 w-3.5" />
				Diff & History
			</Button>
			{#if isHistorical}
				<Button size="sm" class="h-7 text-xs bg-amber-600 hover:bg-amber-700 text-white" onclick={() => navigateToAt(null)}>
					Return to Live
				</Button>
			{/if}
		</div>
	</div>
</div>
