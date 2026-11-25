<script lang="ts">
	import { sourceBadgeClass } from '$lib/utils';

	let { sources = [] }: { sources?: string[] } = $props();
</script>

<div class="flex flex-wrap gap-1">
	{#each sources as src}
		<span class={`inline-flex items-center rounded border px-1.5 py-0.5 font-mono text-[10px] font-medium ${sourceBadgeClass(src)}`}>
			{src}
		</span>
	{/each}
</div>
