<script lang="ts">
	import type { Snippet } from 'svelte';
	import { getContext } from 'svelte';
	import { cn } from '$lib/utils';

	let { value, class: className, children }: { value: string; class?: string; children?: Snippet } = $props();
	const tabs = getContext<{ readonly value: string }>('tabs');
</script>

{#if tabs.value === value}
	<div class={cn(className)}>
		{@render children?.()}
	</div>
{/if}
