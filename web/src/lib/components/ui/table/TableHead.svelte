<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLThAttributes } from 'svelte/elements';
	import { cn } from '$lib/utils';

	let { class: className, children, ...rest }: { class?: string; children?: Snippet } & HTMLThAttributes = $props();
</script>

<th class={cn('h-9 px-3 text-left align-middle text-xs font-medium text-muted-foreground uppercase tracking-wider', className)} {...rest}>
	{@render children?.()}
</th>
