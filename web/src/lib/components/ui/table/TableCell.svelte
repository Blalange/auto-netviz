<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLTdAttributes } from 'svelte/elements';
	import { cn } from '$lib/utils';

	let { class: className, children, ...rest }: { class?: string; children?: Snippet } & HTMLTdAttributes = $props();
</script>

<td class={cn('px-3 py-2.5 align-middle text-sm', className)} {...rest}>
	{@render children?.()}
</td>
