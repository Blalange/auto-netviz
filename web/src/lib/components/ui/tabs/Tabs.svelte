<script lang="ts">
	import type { Snippet } from 'svelte';
	import { setContext } from 'svelte';
	import { cn } from '$lib/utils';

	let { value = $bindable(''), class: className, children }: { value?: string; class?: string; children?: Snippet } = $props();

	setContext('tabs', {
		get value() {
			return value;
		},
		setValue(next: string) {
			value = next;
		}
	});
</script>

<div class={cn('flex flex-col gap-3', className)}>
	{@render children?.()}
</div>
