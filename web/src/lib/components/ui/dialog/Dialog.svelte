<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';

	let {
		open = $bindable(false),
		title = '',
		class: className,
		children
	}: { open?: boolean; title?: string; class?: string; children?: Snippet } = $props();

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && open) open = false;
	}
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
	<div class="fixed inset-0 z-50 flex items-start justify-center bg-black/60 backdrop-blur-xs pt-20 p-4" role="dialog" aria-modal="true">
		<button
			type="button"
			aria-label="Close modal"
			class="fixed inset-0 cursor-default"
			onclick={() => (open = false)}
		></button>
		<div class={cn('relative z-10 w-full max-w-2xl rounded-xl border bg-card p-5 shadow-2xl', className)}>
			{#if title}
				<div class="mb-3 flex items-center justify-between border-b pb-2.5">
					<h2 class="text-sm font-semibold">{title}</h2>
					<button
						type="button"
						onclick={() => (open = false)}
						class="rounded px-2 py-0.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground cursor-pointer"
					>
						ESC
					</button>
				</div>
			{/if}
			{@render children?.()}
		</div>
	</div>
{/if}
