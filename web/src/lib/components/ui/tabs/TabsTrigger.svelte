<script lang="ts">
	import type { Snippet } from 'svelte';
	import { getContext } from 'svelte';
	import { cn } from '$lib/utils';

	let { value, class: className, children }: { value: string; class?: string; children?: Snippet } = $props();
	const tabs = getContext<{ readonly value: string; setValue: (v: string) => void }>('tabs');
	let active = $derived(tabs.value === value);
</script>

<button
	type="button"
	onclick={() => tabs.setValue(value)}
	class={cn(
		'inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-xs font-medium transition-all cursor-pointer',
		active ? 'bg-background text-foreground shadow-xs' : 'hover:text-foreground',
		className
	)}
>
	{@render children?.()}
</button>
