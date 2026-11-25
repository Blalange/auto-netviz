<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLAttributes } from 'svelte/elements';
	import { cn } from '$lib/utils';

	let { class: className, children, ...rest }: { class?: string; children?: Snippet } & HTMLAttributes<HTMLTableRowElement> = $props();
</script>

<tr class={cn('border-b transition-colors hover:bg-muted/40', className)} {...rest}>
	{@render children?.()}
</tr>
