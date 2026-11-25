<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';

	let { class: className, children }: { class?: string; children?: Snippet } = $props();
</script>

<div class="relative w-full overflow-x-auto">
	<table class={cn('w-full caption-bottom text-sm', className)}>
		{@render children?.()}
	</table>
</div>
