<script lang="ts">
	import type { Cable, Device, DeviceType, Rack } from '$lib/api/types';
	import { withAtParam } from '$lib/utils';
	import { CHASSIS_W, RACK_W, RAIL_W, U_PX, layoutDeviceFaceplate, type RenderedGlyph } from './layout';
	import PortGlyph from './PortGlyph.svelte';

	let {
		rack,
		devices = [],
		deviceTypes = {},
		cables = [],
		at = null
	}: {
		rack: Rack;
		devices: Device[];
		deviceTypes: Record<string, DeviceType>;
		cables: Cable[];
		at?: string | null;
	} = $props();

	let selectedDeviceId = $state<string | null>(null);
	let selectedPort = $state<{ deviceId: string; port: string } | null>(null);

	const TOP_PAD = 24;
	const BOT_PAD = 18;
	let totalHeight = $derived(TOP_PAD + rack.u_height * U_PX + BOT_PAD);

	let devMap = $derived.by(() => {
		const m = new Map<string, Device>();
		for (const d of devices) m.set(d.id, d);
		return m;
	});

	function unitY(endU: number): number {
		if (rack.desc_units) {
			return TOP_PAD + (endU - 1) * U_PX;
		}
		return TOP_PAD + (rack.u_height - endU) * U_PX;
	}

	function labelForU(u: number): number {
		return rack.desc_units ? rack.u_height - u + 1 : u;
	}

	function sideForSlot(slotFace: 'front' | 'rear', elevation: 'front' | 'rear'): 'front' | 'rear' {
		// If a device is mounted facing front, the Front elevation sees its front and Rear sees its rear.
		// If a switch is mounted facing rear (e.g. rear ports), the Rear elevation sees its front!
		if (slotFace === elevation) return 'front';
		return 'rear';
	}

	interface PortAnchor {
		x: number;
		y: number;
	}

	function buildElevation(elevation: 'front' | 'rear') {
		const portAnchors = new Map<string, PortAnchor>();
		const slotVisuals = (rack.slots ?? []).map((slot) => {
			const dev = devMap.get(slot.device_id);
			const dt = deviceTypes[slot.device_type];
			const y = unitY(slot.end_u);
			const h = slot.u_height * U_PX;
			const visibleSide = sideForSlot(slot.face, elevation);
			const layout = dev
				? layoutDeviceFaceplate(dev, dt, visibleSide, cables)
				: { glyphs: [] as RenderedGlyph[], heightPx: h, uHeight: slot.u_height };

			for (const g of layout.glyphs) {
				if (g.portName && dev) {
					portAnchors.set(`${dev.id}:${g.portName.toLowerCase()}`, {
						x: RAIL_W + g.x + g.w / 2,
						y: y + g.y + g.h / 2
					});
				}
			}
			return { slot, dev, dt, y, h, visibleSide, glyphs: layout.glyphs };
		});

		// Any cable whose both endpoints exist on this elevation AND involves the selected device/port
		const visibleCables: { cable: Cable; a: PortAnchor; b: PortAnchor }[] = [];
		for (const c of cables) {
			const involvesSelected =
				(selectedDeviceId && (c.a_device_id === selectedDeviceId || c.b_device_id === selectedDeviceId)) ||
				(selectedPort &&
					((c.a_device_id === selectedPort.deviceId && c.a_port.toLowerCase() === selectedPort.port.toLowerCase()) ||
						(c.b_device_id === selectedPort.deviceId && c.b_port.toLowerCase() === selectedPort.port.toLowerCase())));
			if (!involvesSelected) continue;
			const a = portAnchors.get(`${c.a_device_id}:${c.a_port.toLowerCase()}`);
			const b = portAnchors.get(`${c.b_device_id}:${c.b_port.toLowerCase()}`);
			if (a && b) {
				visibleCables.push({ cable: c, a, b });
			}
		}

		return { slotVisuals, visibleCables };
	}

	let frontView = $derived(buildElevation('front'));
	let rearView = $derived(buildElevation('rear'));

	let selectedCables = $derived.by(() => {
		if (!selectedDeviceId && !selectedPort) return [];
		return cables.filter((c) => {
			if (selectedPort) {
				return (
					(c.a_device_id === selectedPort.deviceId && c.a_port.toLowerCase() === selectedPort.port.toLowerCase()) ||
					(c.b_device_id === selectedPort.deviceId && c.b_port.toLowerCase() === selectedPort.port.toLowerCase())
				);
			}
			return c.a_device_id === selectedDeviceId || c.b_device_id === selectedDeviceId;
		});
	});

	function selectSlot(deviceId: string, glyph?: RenderedGlyph) {
		if (glyph?.portName) {
			if (selectedPort?.deviceId === deviceId && selectedPort.port === glyph.portName) {
				selectedPort = null;
			} else {
				selectedPort = { deviceId, port: glyph.portName };
				selectedDeviceId = deviceId;
			}
			return;
		}
		selectedPort = null;
		selectedDeviceId = selectedDeviceId === deviceId ? null : deviceId;
	}

	function curvePath(a: PortAnchor, b: PortAnchor): string {
		const midX = Math.max(a.x, b.x) + 38;
		return `M ${a.x} ${a.y} C ${midX} ${a.y}, ${midX} ${b.y}, ${b.x} ${b.y}`;
	}
</script>

<div class="space-y-4">
	<div class="grid gap-6 xl:grid-cols-2">
		{#each [{ title: 'Front Elevation', side: 'front' as const, data: frontView }, { title: 'Rear Elevation', side: 'rear' as const, data: rearView }] as view}
			<div class="rounded-xl border bg-card p-3 shadow-xs">
				<div class="mb-2 flex items-center justify-between px-1">
					<h3 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{view.title}</h3>
					<span class="font-mono text-[11px] text-muted-foreground">{rack.u_height}U · {rack.width}"</span>
				</div>
				<svg viewBox={`0 0 ${RACK_W} ${totalHeight}`} class="w-full rounded-lg bg-slate-950 border border-slate-800">
					<!-- Rack frame & rails -->
					<rect x="0" y="0" width={RACK_W} height={totalHeight} fill="#090d16" />
					<rect x="0" y={TOP_PAD} width={RAIL_W} height={rack.u_height * U_PX} fill="#111827" stroke="#1e293b" />
					<rect x={RACK_W - RAIL_W} y={TOP_PAD} width={RAIL_W} height={rack.u_height * U_PX} fill="#111827" stroke="#1e293b" />

					<!-- U unit grid rows -->
					{#each Array.from({ length: rack.u_height }, (_, idx) => rack.u_height - idx) as u}
						{@const y = TOP_PAD + (rack.u_height - u) * U_PX}
						<line x1="0" y1={y} x2={RACK_W} y2={y} stroke="#1e293b" stroke-width="0.6" />
						<text x={RAIL_W / 2} y={y + U_PX / 2 + 3} text-anchor="middle" fill="#64748b" font-size="8.5" font-family="monospace">
							{labelForU(u)}
						</text>
						<text x={RACK_W - RAIL_W / 2} y={y + U_PX / 2 + 3} text-anchor="middle" fill="#64748b" font-size="8.5" font-family="monospace">
							{labelForU(u)}
						</text>
					{/each}

					<!-- Mounted Devices -->
					{#each view.data.slotVisuals as item (item.slot.device_id)}
						{@const isSelected = selectedDeviceId === item.slot.device_id}
						<g>
							<!-- Chassis -->
							<rect
								x={RAIL_W}
								y={item.y + 1}
								width={CHASSIS_W}
								height={item.h - 2}
								rx="2"
								fill={item.visibleSide === 'front' ? '#18212f' : '#111827'}
								stroke={isSelected ? '#38bdf8' : '#334155'}
								stroke-width={isSelected ? 1.8 : 0.9}
							/>
							<!-- Rack ears -->
							<rect x={RAIL_W} y={item.y + 1} width="12" height={item.h - 2} fill="#273244" />
							<rect x={RAIL_W + CHASSIS_W - 12} y={item.y + 1} width="12" height={item.h - 2} fill="#273244" />

							<!-- Glyphs -->
							<g transform={`translate(${RAIL_W}, ${item.y})`}>
								{#each item.glyphs as glyph (glyph.id)}
									<PortGlyph
										{glyph}
										selected={Boolean(
											selectedPort &&
												selectedPort.deviceId === item.slot.device_id &&
												glyph.portName?.toLowerCase() === selectedPort.port.toLowerCase()
										)}
										onSelect={(g) => selectSlot(item.slot.device_id, g)}
									/>
								{/each}
							</g>

							<!-- Clickable device nameplate pill -->
							<!-- svelte-ignore a11y_click_events_have_key_events -->
							<!-- svelte-ignore a11y_no_static_element_interactions -->
							<g class="cursor-pointer" onclick={() => selectSlot(item.slot.device_id)}>
								<rect
									x={RAIL_W + CHASSIS_W - 122}
									y={item.y + 3}
									width="106"
									height="14"
									rx="2.5"
									fill="#020617"
									fill-opacity="0.86"
									stroke={isSelected ? '#38bdf8' : '#334155'}
									stroke-width="0.7"
								/>
								<circle
									cx={RAIL_W + CHASSIS_W - 115}
									cy={item.y + 10}
									r="2.2"
									fill={item.slot.status === 'active' ? '#10b981' : '#ef4444'}
								/>
								<text
									x={RAIL_W + CHASSIS_W - 109}
									y={item.y + 12.5}
									fill="#e2e8f0"
									font-size="8"
									font-family="monospace"
									font-weight="600"
								>
									{item.slot.device_name.slice(0, 16)}
								</text>
							</g>
						</g>
					{/each}

					<!-- Animated Cable Trace Overlays -->
					{#each view.data.visibleCables as vc (vc.cable.id)}
						<path
							d={curvePath(vc.a, vc.b)}
							fill="none"
							stroke={vc.cable.color || '#38bdf8'}
							stroke-width="2.4"
							stroke-linecap="round"
							class="animate-cable-flow pointer-events-none"
						/>
						<circle cx={vc.a.x} cy={vc.a.y} r="3.5" fill={vc.cable.color || '#38bdf8'} />
						<circle cx={vc.b.x} cy={vc.b.y} r="3.5" fill={vc.cable.color || '#38bdf8'} />
					{/each}
				</svg>
			</div>
		{/each}
	</div>

	{#if selectedDeviceId}
		{@const selDev = devMap.get(selectedDeviceId)}
		{#if selDev}
			<div class="rounded-xl border bg-card p-4">
				<div class="flex flex-wrap items-center justify-between gap-2 pb-3 border-b">
					<div class="flex items-center gap-2.5">
						<span class="h-2.5 w-2.5 rounded-full {selDev.status === 'active' ? 'bg-emerald-500' : 'bg-rose-500'}"></span>
						<a href={withAtParam(`/devices/${encodeURIComponent(selDev.id)}`, at)} class="text-sm font-semibold hover:underline">
							{selDev.name}
						</a>
						<span class="font-mono text-xs text-muted-foreground">{selDev.role} · U{selDev.position}–U{selDev.end_position} ({selDev.face})</span>
						{#if selectedPort}
							<span class="rounded bg-sky-500/15 border border-sky-500/30 px-2 py-0.5 font-mono text-xs text-sky-400">
								Port: {selectedPort.port}
							</span>
						{/if}
					</div>
					<a href={withAtParam(`/devices/${encodeURIComponent(selDev.id)}`, at)} class="text-xs font-medium text-primary hover:underline">
						Open Device Detail →
					</a>
				</div>
				{#if selectedCables.length > 0}
					<div class="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
						{#each selectedCables as c}
							<div class="flex items-center justify-between rounded-lg border bg-muted/40 px-3 py-2 text-xs font-mono">
								<span>{c.a_device}:{c.a_port}</span>
								<span class="text-primary">↔</span>
								<span>{c.b_device}:{c.b_port}</span>
							</div>
						{/each}
					</div>
				{:else}
					<p class="mt-3 text-xs text-muted-foreground">Click any port glyph above to trace its patch cable.</p>
				{/if}
			</div>
		{/if}
	{/if}
</div>
