<script lang="ts">
	import type { RenderedGlyph } from './layout';
	import { vlanColor } from './layout';

	let {
		glyph,
		selected = false,
		onSelect
	}: {
		glyph: RenderedGlyph;
		selected?: boolean;
		onSelect?: (glyph: RenderedGlyph) => void;
	} = $props();

	let isUp = $derived(glyph.iface?.link_up === true || Boolean(glyph.cable));
	let isDisabled = $derived(glyph.iface && !glyph.iface.enabled);
	let ledColor = $derived(
		isDisabled ? '#475569' : isUp ? '#10b981' : glyph.cable ? '#f59e0b' : '#334155'
	);
	let ringStroke = $derived(selected ? '#38bdf8' : glyph.cable ? '#94a3b8' : '#334155');
	let vlanTick = $derived(vlanColor(glyph.iface?.untagged_vlan));
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<g
	class={glyph.portName || glyph.module ? 'cursor-pointer' : ''}
	onclick={() => onSelect?.(glyph)}
>
	<title>
		{glyph.label}{glyph.iface?.speed_mbps ? ` · ${glyph.iface.speed_mbps} Mbps` : ''}{glyph.iface?.untagged_vlan ? ` · VLAN ${glyph.iface.untagged_vlan}` : ''}{glyph.cable ? ` ↔ ${glyph.cable.a_port === glyph.portName ? glyph.cable.b_device + ' ' + glyph.cable.b_port : glyph.cable.a_device + ' ' + glyph.cable.a_port}` : ''}{glyph.module ? ` · ${glyph.module.model || ''} (${glyph.module.serial || 'no serial'})` : ''}
	</title>

	{#if glyph.kind === 'lcd'}
		<rect x={glyph.x} y={glyph.y} width={glyph.w} height={glyph.h} rx="2" fill="#0f172a" stroke="#38bdf8" stroke-width="0.9" />
		<text x={glyph.x + glyph.w / 2} y={glyph.y + glyph.h / 2 + 3} text-anchor="middle" fill="#7dd3fc" font-size="7" font-family="monospace" font-weight="700">
			{glyph.label}
		</text>
	{:else if glyph.kind === 'label'}
		<rect x={glyph.x} y={glyph.y} width={glyph.w} height={glyph.h} rx="2" fill="#1e293b" stroke="#334155" stroke-width="0.8" />
		<text x={glyph.x + 5} y={glyph.y + glyph.h / 2 + 3} fill="#94a3b8" font-size="8" font-family="monospace">
			{glyph.label.slice(0, 18)}
		</text>
	{:else if glyph.kind === 'rj45' || glyph.kind === 'mgmt'}
		<rect
			x={glyph.x}
			y={glyph.y}
			width={glyph.w}
			height={glyph.h}
			rx="1.5"
			fill={glyph.kind === 'mgmt' ? '#164e63' : '#090d16'}
			stroke={ringStroke}
			stroke-width={selected ? 1.6 : 0.9}
		/>
		<!-- RJ45 inner notch -->
		<rect x={glyph.x + 2.5} y={glyph.y + 3} width={glyph.w - 5} height={glyph.h - 6.5} rx="0.8" fill="#020617" stroke="#1e293b" stroke-width="0.5" />
		<!-- Status LED -->
		<circle cx={glyph.x + 2.5} cy={glyph.y + 2.2} r="1.3" fill={ledColor} />
		<!-- PoE indicator -->
		{#if glyph.poe}
			<circle cx={glyph.x + glyph.w - 2.5} cy={glyph.y + 2.2} r="1.2" fill="#f59e0b" />
		{/if}
		<!-- VLAN color bar -->
		{#if glyph.iface?.untagged_vlan}
			<rect x={glyph.x + 1} y={glyph.y + glyph.h - 2} width={glyph.w - 2} height="1.5" fill={vlanTick} />
		{/if}
	{:else if glyph.kind === 'sfp' || glyph.kind === 'sfpp' || glyph.kind === 'qsfp28'}
		<rect
			x={glyph.x}
			y={glyph.y}
			width={glyph.w}
			height={glyph.h}
			rx="1.5"
			fill="#1e293b"
			stroke={selected ? '#38bdf8' : glyph.cable ? '#f59e0b' : '#475569'}
			stroke-width={selected ? 1.6 : 1}
		/>
		<rect x={glyph.x + 2} y={glyph.y + 2.5} width={glyph.w - 4} height={glyph.h - 5} fill="#020617" />
		<circle cx={glyph.x + glyph.w - 2.5} cy={glyph.y + 2.5} r="1.4" fill={ledColor} />
	{:else if glyph.kind === 'drive'}
		<rect
			x={glyph.x}
			y={glyph.y}
			width={glyph.w}
			height={glyph.h}
			rx="1.5"
			fill={glyph.module ? '#1e293b' : '#0f172a'}
			stroke={selected ? '#38bdf8' : glyph.module ? '#475569' : '#1e293b'}
			stroke-width="0.9"
		/>
		<!-- Drive latch handle -->
		<rect x={glyph.x + 2} y={glyph.y + 3} width="3" height={glyph.h - 6} rx="0.8" fill={glyph.module ? '#0ea5e9' : '#334155'} />
		<circle cx={glyph.x + glyph.w - 3.5} cy={glyph.y + 4} r="1.3" fill={glyph.module ? '#10b981' : '#334155'} />
		{#if glyph.w >= 26}
			<text x={glyph.x + 7} y={glyph.y + glyph.h / 2 + 2.5} fill="#cbd5e1" font-size="6.5" font-family="monospace">
				{glyph.module ? 'DISK' : 'EMPTY'}
			</text>
		{/if}
	{:else if glyph.kind === 'psu'}
		<rect
			x={glyph.x}
			y={glyph.y}
			width={glyph.w}
			height={glyph.h}
			rx="2"
			fill="#18181b"
			stroke={glyph.module?.status === 'failed' ? '#ef4444' : '#52525b'}
			stroke-width="1"
		/>
		<rect x={glyph.x + 4} y={glyph.y + 4} width="12" height={glyph.h - 8} rx="1" fill="#27272a" stroke="#3f3f46" stroke-width="0.6" />
		<circle cx={glyph.x + glyph.w - 5} cy={glyph.y + 6} r="2" fill={glyph.module?.status === 'failed' ? '#ef4444' : '#10b981'} />
		<text x={glyph.x + 19} y={glyph.y + glyph.h - 5} fill="#a1a1aa" font-size="6" font-family="monospace">PSU</text>
	{:else if glyph.kind === 'power'}
		<rect x={glyph.x} y={glyph.y} width={glyph.w} height={glyph.h} rx="1.5" fill="#090d16" stroke="#475569" stroke-width="0.9" />
		<line x1={glyph.x + 5} y1={glyph.y + glyph.h - 4} x2={glyph.x + 5} y2={glyph.y + glyph.h - 8} stroke="#94a3b8" stroke-width="1.3" />
		<line x1={glyph.x + glyph.w / 2} y1={glyph.y + 4} x2={glyph.x + glyph.w / 2} y2={glyph.y + 8} stroke="#94a3b8" stroke-width="1.3" />
		<line x1={glyph.x + glyph.w - 5} y1={glyph.y + glyph.h - 4} x2={glyph.x + glyph.w - 5} y2={glyph.y + glyph.h - 8} stroke="#94a3b8" stroke-width="1.3" />
	{/if}
</g>
