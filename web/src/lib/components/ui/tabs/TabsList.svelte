<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';

	let { class: className, children }: { class?: string; children?: Snippet } = $props();
</script>

<div class={cn('inline-flex h-9 items-center justify-start rounded-lg bg-muted p-1 text-muted-foreground w-fit', className)}>
	{@render children?.()}
</div>
