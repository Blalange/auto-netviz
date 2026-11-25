<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';

	let { class: className, children }: { class?: string; children?: Snippet } = $props();
</script>

<thead class={cn('[&_tr]:border-b bg-muted/40', className)}>
	{@render children?.()}
</thead>
