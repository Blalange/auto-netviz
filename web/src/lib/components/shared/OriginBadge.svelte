<script lang="ts">
	import Badge from '$lib/components/ui/badge/Badge.svelte';

	let { origin }: { origin: string } = $props();
</script>

{#if origin === 'auto'}
	<Badge class="bg-purple-500/15 text-purple-700 dark:text-purple-300 border-purple-500/30">Auto-Discovered</Badge>
{:else if origin === 'merged'}
	<Badge class="bg-sky-500/15 text-sky-700 dark:text-sky-300 border-sky-500/30">Manual + Live</Badge>
{:else}
	<Badge variant="outline">Manual Only</Badge>
{/if}
