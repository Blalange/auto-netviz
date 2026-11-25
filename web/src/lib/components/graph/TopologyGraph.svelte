<script lang="ts">
	import { onMount } from 'svelte';
	import { SvelteFlow, Background, Controls, MiniMap, type Node, type Edge } from '@xyflow/svelte';
	import type { Cable } from '$lib/api/types';
	import TopologyNode from './TopologyNode.svelte';

	interface RawNode {
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
	}

	let {
		rawNodes = [],
		rawEdges = [],
		at = null,
		highlightQuery = ''
	}: {
		rawNodes: RawNode[];
		rawEdges: Cable[];
		at?: string | null;
		highlightQuery?: string;
	} = $props();

	let nodes = $state.raw<Node[]>([]);
	let edges = $state.raw<Edge[]>([]);
	let mounted = $state(false);
	const nodeTypes = { device: TopologyNode };

	const roleTier: Record<string, number> = {
		gateway: 0,
		router: 0,
		switch: 1,
		'access-point': 2,
		'control-plane': 2,
		worker: 2,
		server: 2,
		storage: 2,
		power: 2,
		appliance: 3,
		workstation: 3,
		unknown: 3
	};

	async function layoutGraph(rNodes: RawNode[], rEdges: Cable[], q: string, curAt: string | null) {
		const lowerQ = q.trim().toLowerCase();
		let positions = new Map<string, { x: number; y: number }>();

		try {
			const ELK = (await import('elkjs/lib/elk.bundled.js')).default;
			const elk = new ELK();
			const elkGraph = {
				id: 'root',
				layoutOptions: {
					'elk.algorithm': 'layered',
					'elk.direction': 'DOWN',
					'elk.spacing.nodeNode': '48',
					'elk.layered.spacing.nodeNodeBetweenLayers': '88'
				},
				children: rNodes.map((n) => ({
					id: n.id,
					width: 210,
					height: 82,
					layoutOptions: {
						'elk.layered.priority.direction': String(10 - (roleTier[n.role] ?? 3))
					}
				})),
				edges: rEdges.map((e) => ({
					id: e.id,
					sources: [e.a_device_id],
					targets: [e.b_device_id]
				}))
			};
			const res = await elk.layout(elkGraph);
			for (const child of res.children ?? []) {
				positions.set(child.id, { x: child.x ?? 0, y: child.y ?? 0 });
			}
		} catch {
			// Deterministic grid fallback if ELK fails
			const tiers: Record<number, number> = {};
			for (const n of rNodes) {
				const t = roleTier[n.role] ?? 2;
				const col = tiers[t] ?? 0;
				tiers[t] = col + 1;
				positions.set(n.id, { x: col * 240, y: t * 140 });
			}
		}

		nodes = rNodes.map((n, idx) => {
			const pos = positions.get(n.id) ?? { x: (idx % 4) * 240, y: Math.floor(idx / 4) * 140 };
			const highlighted = Boolean(
				lowerQ && (n.name.toLowerCase().includes(lowerQ) || (n.primary_ip || '').toLowerCase().includes(lowerQ))
			);
			return {
				id: n.id,
				type: 'device',
				position: pos,
				data: { ...n, at: curAt, highlighted }
			};
		});

		edges = rEdges.map((e) => {
			const stroke =
				e.color ||
				(e.confidence === 'manual'
					? '#38bdf8'
					: e.confidence === 'verified'
						? '#10b981'
						: e.confidence === 'inferred'
							? '#a855f7'
							: '#64748b');
			return {
				id: e.id,
				source: e.a_device_id,
				target: e.b_device_id,
				label: `${e.a_port} ↔ ${e.b_port}`,
				animated: e.confidence === 'verified' || e.speed_mbps === 10000,
				style: `stroke: ${stroke}; stroke-width: ${e.speed_mbps && e.speed_mbps >= 10000 ? 2.5 : 1.5};${
					e.confidence === 'inferred' ? ' stroke-dasharray: 5 4;' : ''
				}`
			};
		});
	}

	onMount(() => {
		mounted = true;
	});

	$effect(() => {
		if (!mounted) return;
		layoutGraph(rawNodes, rawEdges, highlightQuery, at);
	});
</script>

<div class="h-[680px] w-full rounded-xl border bg-card overflow-hidden">
	{#if mounted}
		<SvelteFlow {nodes} {edges} {nodeTypes} fitView minZoom={0.2} maxZoom={2.5}>
			<Background />
			<Controls />
			<MiniMap />
		</SvelteFlow>
	{:else}
		<div class="flex h-full items-center justify-center text-sm text-muted-foreground">
			Initializing topology canvas...
		</div>
	{/if}
</div>
