<script lang="ts">
	import Dialog from '$lib/components/ui/dialog/Dialog.svelte';
	import Button from '$lib/components/ui/button/Button.svelte';
	import { Check, Copy } from '@lucide/svelte';

	let { open = $bindable(false), deviceName = '', yamlSnippet = '' }: { open?: boolean; deviceName?: string; yamlSnippet?: string } = $props();
	let copied = $state(false);

	async function copyText() {
		try {
			await navigator.clipboard.writeText(yamlSnippet);
			copied = true;
			setTimeout(() => (copied = false), 2000);
		} catch {
			// ignore clipboard failures in non-https contexts
		}
	}
</script>

<Dialog bind:open title={`Manual YAML Snippet — ${deviceName}`}>
	<p class="mb-3 text-xs text-muted-foreground">
		Auto Netviz is strictly read-only. Paste this snippet into a YAML file in your manual inventory directory to claim, customize, or rack-mount this device.
	</p>
	<pre class="max-h-96 overflow-auto rounded-lg border bg-muted/60 p-3 font-mono text-xs leading-relaxed">{yamlSnippet}</pre>
	<div class="mt-4 flex justify-end gap-2">
		<Button variant="outline" size="sm" onclick={() => (open = false)}>Close</Button>
		<Button size="sm" onclick={copyText}>
			{#if copied}
				<Check class="h-3.5 w-3.5" />
				Copied!
			{:else}
				<Copy class="h-3.5 w-3.5" />
				Copy YAML
			{/if}
		</Button>
	</div>
</Dialog>
