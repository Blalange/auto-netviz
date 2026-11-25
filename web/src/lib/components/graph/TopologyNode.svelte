<script lang="ts">
	import { Handle, Position } from '@xyflow/svelte';
	import { statusColorClass, withAtParam } from '$lib/utils';

	interface NodeData {
		id: string;
		name: string;
		role: string;
		role_color?: string;
		status: string;
		origin: string;
		primary_ip?: string;
		owner?: string;
		port_count?: number;
		diffState?: 'added' | 'removed' | 'modified' | null;
		at?: string | null;
		highlighted?: boolean;
	}

	let { data }: { data: NodeData } = $props();

	let diffBorder = $derived(
		data.diffState === 'added'
			? 'ring-2 ring-emerald-500 border-emerald-500'
			: data.diffState === 'removed'
				? 'ring-2 ring-rose-500 border-rose-500 opacity-75'
				: data.diffState === 'modified'
					? 'ring-2 ring-amber-500 border-amber-500'
					: data.highlighted
						? 'ring-2 ring-sky-400 border-sky-400'
						: 'border-border'
	);
</script>

<div class={`min-w-[190px] rounded-xl border bg-card px-3 py-2.5 shadow-md transition-all ${diffBorder}`}>
	<Handle type="target" position={Position.Top} class="!bg-slate-400 !w-2 !h-2" />

	<div class="flex items-center justify-between gap-2">
		<div class="flex items-center gap-2 min-w-0">
			<span class="h-2.5 w-2.5 rounded-full shrink-0" style={`background-color: ${data.role_color || '#64748b'}`}></span>
			<a
				href={withAtParam(`/devices/${encodeURIComponent(data.id)}`, data.at)}
				class="text-xs font-semibold truncate hover:underline text-foreground"
			>
				{data.name}
			</a>
		</div>
		<span class={`h-2 w-2 rounded-full shrink-0 ${statusColorClass(data.status)}`} title={data.status}></span>
	</div>

	<div class="mt-1.5 flex items-center justify-between gap-2 text-[10px] font-mono text-muted-foreground">
		<span>{data.role}</span>
		{#if data.primary_ip}
			<span class="text-foreground/80">{data.primary_ip}</span>
		{/if}
	</div>

	<div class="mt-1.5 flex items-center justify-between gap-1.5">
		{#if data.owner}
			<span class="truncate rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{data.owner}</span>
		{:else}
			<span></span>
		{/if}
		<div class="flex items-center gap-1">
			{#if data.origin === 'auto'}
				<span class="rounded bg-purple-500/15 border border-purple-500/30 px-1 py-0.5 text-[9px] font-mono text-purple-600 dark:text-purple-300">
					AUTO
				</span>
			{/if}
			{#if data.diffState}
				<span
					class={`rounded px-1 py-0.5 text-[9px] font-mono uppercase font-semibold ${
						data.diffState === 'added'
							? 'bg-emerald-500/20 text-emerald-500'
							: data.diffState === 'removed'
								? 'bg-rose-500/20 text-rose-500'
								: 'bg-amber-500/20 text-amber-500'
					}`}
				>
					{data.diffState}
				</span>
			{/if}
		</div>
	</div>

	<Handle type="source" position={Position.Bottom} class="!bg-slate-400 !w-2 !h-2" />
</div>
