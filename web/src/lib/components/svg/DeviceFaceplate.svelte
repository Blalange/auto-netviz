<script lang="ts">
	import type { Cable, Device, DeviceType } from '$lib/api/types';
	import { CHASSIS_W, layoutDeviceFaceplate, type RenderedGlyph } from './layout';
	import PortGlyph from './PortGlyph.svelte';

	let {
		device,
		deviceType,
		side = 'front',
		cables = [],
		selectedPort = $bindable<string | null>(null),
		onPortClick
	}: {
		device: Device;
		deviceType?: DeviceType;
		side?: 'front' | 'rear';
		cables?: Cable[];
		selectedPort?: string | null;
		onPortClick?: (glyph: RenderedGlyph) => void;
	} = $props();

	let layout = $derived(layoutDeviceFaceplate(device, deviceType, side, cables));
	let activeGlyph = $derived(
		layout.glyphs.find((g) => g.portName && selectedPort && g.portName.toLowerCase() === selectedPort.toLowerCase()) ?? null
	);

	function handleSelect(g: RenderedGlyph) {
		if (g.portName) {
			selectedPort = selectedPort === g.portName ? null : g.portName;
		}
		onPortClick?.(g);
	}
</script>

<div class="space-y-2">
	<svg
		viewBox={`0 0 ${CHASSIS_W} ${layout.heightPx}`}
		class="w-full rounded-md border bg-slate-950 shadow-inner"
		role="img"
		aria-label={`${device.name} ${side} elevation`}
	>
		<!-- Metallic chassis body -->
		<rect x="0" y="0" width={CHASSIS_W} height={layout.heightPx} fill="#18212f" />
		<!-- Left & right rack mounting ears -->
		<rect x="0" y="0" width="12" height={layout.heightPx} fill="#273244" stroke="#334155" stroke-width="0.8" />
		<rect x={CHASSIS_W - 12} y="0" width="12" height={layout.heightPx} fill="#273244" stroke="#334155" stroke-width="0.8" />
		<circle cx="6" cy="8" r="2" fill="#0f172a" stroke="#475569" stroke-width="0.6" />
		<circle cx="6" cy={layout.heightPx - 8} r="2" fill="#0f172a" stroke="#475569" stroke-width="0.6" />
		<circle cx={CHASSIS_W - 6} cy="8" r="2" fill="#0f172a" stroke="#475569" stroke-width="0.6" />
		<circle cx={CHASSIS_W - 6} cy={layout.heightPx - 8} r="2" fill="#0f172a" stroke="#475569" stroke-width="0.6" />

		<!-- Device status LED -->
		<circle cx="15" cy="7" r="1.8" fill={device.status === 'active' ? '#10b981' : '#ef4444'} />

		{#each layout.glyphs as glyph (glyph.id)}
			<PortGlyph
				{glyph}
				selected={Boolean(glyph.portName && selectedPort && glyph.portName.toLowerCase() === selectedPort.toLowerCase())}
				onSelect={handleSelect}
			/>
		{/each}
	</svg>

	{#if activeGlyph}
		<div class="rounded-lg border bg-muted/50 px-3 py-2 text-xs flex flex-wrap items-center justify-between gap-2">
			<div class="flex items-center gap-2 font-mono">
				<span class="font-semibold text-foreground">{activeGlyph.portName}</span>
				{#if activeGlyph.iface?.type}
					<span class="rounded bg-background px-1.5 py-0.5 border text-[10px]">{activeGlyph.iface.type}</span>
				{/if}
				{#if activeGlyph.iface?.speed_mbps}
					<span class="text-emerald-600 dark:text-emerald-400">{activeGlyph.iface.speed_mbps} Mbps</span>
				{/if}
				{#if activeGlyph.iface?.untagged_vlan}
					<span class="text-sky-600 dark:text-sky-400">VLAN {activeGlyph.iface.untagged_vlan}</span>
				{/if}
				{#if activeGlyph.iface?.poe_watts}
					<span class="text-amber-600 dark:text-amber-400">{activeGlyph.iface.poe_watts}W PoE</span>
				{/if}
			</div>
			{#if activeGlyph.cable}
				{@const peer =
					activeGlyph.cable.a_device_id === device.id
						? `${activeGlyph.cable.b_device} : ${activeGlyph.cable.b_port}`
						: `${activeGlyph.cable.a_device} : ${activeGlyph.cable.a_port}`}
				<span class="font-mono text-primary font-medium">Connected ↔ {peer} ({activeGlyph.cable.type})</span>
			{:else}
				<span class="text-muted-foreground">No cable connected</span>
			{/if}
		</div>
	{/if}
</div>
