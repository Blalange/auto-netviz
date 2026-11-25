import type { Cable, Device, DeviceModule, DeviceType, FaceplateElement, InterfaceItem } from '$lib/api/types';

export const CHASSIS_W = 460;
export const U_PX = 38;
export const RAIL_W = 28;
export const RACK_W = CHASSIS_W + RAIL_W * 2;

export interface RenderedGlyph {
	id: string;
	kind: 'lcd' | 'rj45' | 'sfp' | 'sfpp' | 'qsfp28' | 'mgmt' | 'drive' | 'psu' | 'power' | 'fan' | 'label';
	x: number;
	y: number;
	w: number;
	h: number;
	label: string;
	portName?: string;
	iface?: InterfaceItem;
	cable?: Cable;
	module?: DeviceModule;
	poe?: boolean;
	formFactor?: string;
}

export function cableForPort(cables: Cable[], deviceId: string, portName?: string): Cable | undefined {
	if (!portName) return undefined;
	const target = portName.toLowerCase();
	return cables.find(
		(c) =>
			(c.a_device_id === deviceId && c.a_port.toLowerCase() === target) ||
			(c.b_device_id === deviceId && c.b_port.toLowerCase() === target)
	);
}

export function vlanColor(vid?: number): string {
	if (!vid) return '#64748b';
	const palette = ['#3b82f6', '#10b981', '#f59e0b', '#8b5cf6', '#ec4899', '#06b6d4', '#84cc16', '#f97316'];
	return palette[vid % palette.length];
}

export function layoutDeviceFaceplate(
	device: Device,
	dt: DeviceType | undefined,
	side: 'front' | 'rear',
	cables: Cable[]
): { glyphs: RenderedGlyph[]; heightPx: number; uHeight: number } {
	const uHeight = Math.max(1, dt?.u_height ?? 1);
	const heightPx = uHeight * U_PX;

	let elements: FaceplateElement[] = [];
	if (dt?.faceplate) {
		elements = side === 'front' ? dt.faceplate.front ?? [] : dt.faceplate.rear ?? [];
	}

	// Fallback synthesis when faceplate wasn't explicitly specified on that side
	if (elements.length === 0) {
		elements = synthesizeElements(device, dt, side);
	}

	const ifaceByName = new Map<string, InterfaceItem>();
	for (const iface of device.interfaces ?? []) {
		ifaceByName.set(iface.name.toLowerCase(), iface);
	}
	const modByBay = new Map<string, DeviceModule>();
	for (const mod of device.modules ?? []) {
		modByBay.set(mod.bay.toLowerCase(), mod);
	}

	const glyphs: RenderedGlyph[] = [];
	const padX = 18; // leave space for rack ears
	const padY = 4;
	const innerH = heightPx - padY * 2;
	let curX = padX;

	for (let idx = 0; idx < elements.length; idx++) {
		const el = elements[idx];
		if (curX >= CHASSIS_W - padX) break;

		switch (el.kind) {
			case 'lcd': {
				const w = (el.width || 2) * 14;
				const h = Math.min(26, innerH - 4);
				glyphs.push({
					id: `lcd-${idx}`,
					kind: 'lcd',
					x: curX,
					y: padY + (innerH - h) / 2,
					w,
					h,
					label: el.label || device.name.slice(0, 6)
				});
				curX += w + 8;
				break;
			}
			case 'label': {
				const w = Math.min(120, Math.max(48, (el.label?.length || 6) * 7 + 12));
				glyphs.push({
					id: `lbl-${idx}`,
					kind: 'label',
					x: curX,
					y: padY + (innerH - 18) / 2,
					w,
					h: 18,
					label: el.label || device.model || device.name
				});
				curX += w + 8;
				break;
			}
			case 'port_grid': {
				const rows = Math.max(1, el.rows || 1);
				const names = el.names ?? [];
				const cols = Math.max(1, el.cols || (names.length > 0 ? Math.ceil(names.length / rows) : 1));
				const isSfp = (el.connector || '').includes('sfp');
				const cellW = isSfp ? 17 : 14;
				const cellH = Math.min(isSfp ? 14 : 13, Math.floor((innerH - (rows - 1) * 2) / rows));
				const gapX = 2;
				const gapY = 2;
				const totalH = rows * cellH + (rows - 1) * gapY;
				const startY = padY + (innerH - totalH) / 2;

				// Switch grids typically number vertically top-to-bottom per column (1,2 in col 0; 3,4 in col 1)
				let n = 0;
				const startNum = el.start ?? 1;
				const prefix = el.prefix ?? 'Port ';

				for (let c = 0; c < cols; c++) {
					for (let r = 0; r < rows; r++) {
						const portName = names.length > 0 ? names[n] : `${prefix}${startNum + n}`;
						if (!portName) continue;
						n++;
						const x = curX + c * (cellW + gapX);
						const y = startY + r * (cellH + gapY);
						if (x + cellW > CHASSIS_W - padX) continue;
						const iface = ifaceByName.get(portName.toLowerCase());
						const cable = cableForPort(cables, device.id, portName);
						const connKind =
							el.connector === 'qsfp28'
								? 'qsfp28'
								: el.connector === 'sfpp'
									? 'sfpp'
									: el.connector === 'sfp'
										? 'sfp'
										: 'rj45';
						glyphs.push({
							id: `port-${idx}-${portName}`,
							kind: connKind,
							x,
							y,
							w: cellW,
							h: cellH,
							label: portName,
							portName,
							iface,
							cable,
							poe: el.poe || iface?.poe_mode === 'pse'
						});
					}
				}
				curX += cols * (cellW + gapX) + 6;
				break;
			}
			case 'mgmt_port': {
				const portName = el.names?.[0] || el.label || 'iDRAC';
				const w = 18;
				const h = Math.min(18, innerH - 4);
				const iface = ifaceByName.get(portName.toLowerCase());
				const cable = cableForPort(cables, device.id, portName);
				glyphs.push({
					id: `mgmt-${idx}-${portName}`,
					kind: 'mgmt',
					x: curX,
					y: padY + (innerH - h) / 2,
					w,
					h,
					label: portName,
					portName,
					iface,
					cable
				});
				curX += w + 8;
				break;
			}
			case 'drive_bays': {
				const count = Math.max(1, el.count || 4);
				const names = el.names ?? [];
				const prefix = el.prefix ?? 'Drive ';
				const isLFF = el.form_factor === '3.5';
				const rows = isLFF && uHeight >= 2 ? 3 : 1;
				const cols = Math.ceil(count / rows);
				const availW = CHASSIS_W - padX - curX - 4;
				const bayW = Math.min(isLFF ? 48 : 30, Math.max(16, Math.floor((availW - (cols - 1) * 3) / cols)));
				const bayH = Math.min(24, Math.floor((innerH - (rows - 1) * 3) / rows));
				const startY = padY + (innerH - (rows * bayH + (rows - 1) * 3)) / 2;

				for (let i = 0; i < count; i++) {
					const r = Math.floor(i / cols);
					const c = i % cols;
					const x = curX + c * (bayW + 3);
					const y = startY + r * (bayH + 3);
					if (x + bayW > CHASSIS_W - padX) continue;
					const bayName = names[i] || `${prefix}${i + 1}`;
					const mod = modByBay.get(bayName.toLowerCase());
					glyphs.push({
						id: `drv-${idx}-${bayName}`,
						kind: 'drive',
						x,
						y,
						w: bayW,
						h: bayH,
						label: bayName,
						module: mod,
						formFactor: el.form_factor || '2.5'
					});
				}
				curX += cols * (bayW + 3) + 8;
				break;
			}
			case 'psu_bays': {
				const count = Math.max(1, el.count || 2);
				const names = el.names ?? [];
				const prefix = el.prefix ?? 'PSU ';
				const w = 36;
				const h = Math.min(26, innerH - 4);
				for (let i = 0; i < count; i++) {
					const x = curX + i * (w + 4);
					if (x + w > CHASSIS_W - padX) continue;
					const bayName = names[i] || `${prefix}${i + 1}`;
					const mod = modByBay.get(bayName.toLowerCase());
					glyphs.push({
						id: `psu-${idx}-${bayName}`,
						kind: 'psu',
						x,
						y: padY + (innerH - h) / 2,
						w,
						h,
						label: bayName,
						module: mod
					});
				}
				curX += count * (w + 4) + 8;
				break;
			}
			case 'power': {
				const count = Math.max(1, el.count || 1);
				const w = 18;
				const h = Math.min(16, innerH - 6);
				for (let i = 0; i < count; i++) {
					const x = curX + i * (w + 4);
					if (x + w > CHASSIS_W - padX) continue;
					glyphs.push({
						id: `pwr-${idx}-${i}`,
						kind: 'power',
						x,
						y: padY + (innerH - h) / 2,
						w,
						h,
						label: `PWR ${i + 1}`
					});
				}
				curX += count * (w + 4) + 8;
				break;
			}
		}
	}

	return { glyphs, heightPx, uHeight };
}

function synthesizeElements(device: Device, dt: DeviceType | undefined, side: 'front' | 'rear'): FaceplateElement[] {
	const isNet = device.role === 'switch' || device.role === 'gateway' || device.role === 'router' || device.role === 'access-point';
	const rj45: string[] = [];
	const sfp: string[] = [];
	const mgmt: string[] = [];
	for (const iface of device.interfaces ?? []) {
		if (iface.kind === 'virtual' || iface.kind === 'wireless' || iface.kind === 'lag') continue;
		if (iface.mgmt_only) {
			mgmt.push(iface.name);
		} else if ((iface.type || '').includes('sfp')) {
			sfp.push(iface.name);
		} else {
			rj45.push(iface.name);
		}
	}
	const drives = (device.modules ?? []).filter((m) => m.profile?.toLowerCase().includes('disk')).map((m) => m.bay);
	const psus = (device.modules ?? []).filter((m) => m.profile?.toLowerCase().includes('power')).map((m) => m.bay);

	if (isNet) {
		if (side === 'rear') {
			return [{ kind: 'label', label: dt?.model || device.role }, { kind: 'power', connector: 'iec-c14', count: 1 }];
		}
		const out: FaceplateElement[] = [{ kind: 'lcd', label: device.role.slice(0, 3).toUpperCase(), width: 2 }];
		if (rj45.length > 0) {
			const rows = rj45.length >= 8 ? 2 : 1;
			out.push({ kind: 'port_grid', connector: 'rj45', rows, cols: Math.ceil(rj45.length / rows), names: rj45 });
		}
		if (sfp.length > 0) {
			const rows = sfp.length >= 2 ? 2 : 1;
			out.push({ kind: 'port_grid', connector: 'sfpp', rows, cols: Math.ceil(sfp.length / rows), names: sfp });
		}
		return out;
	}

	// Server / storage / power
	if (side === 'front') {
		if (drives.length > 0) {
			return [{ kind: 'drive_bays', count: drives.length, names: drives, form_factor: '2.5' }];
		}
		return [{ kind: 'label', label: dt?.model || device.name }];
	}
	const rearOut: FaceplateElement[] = [];
	if (psus.length > 0) {
		rearOut.push({ kind: 'psu_bays', count: psus.length, names: psus });
	}
	for (const m of mgmt) {
		rearOut.push({ kind: 'mgmt_port', label: m, names: [m] });
	}
	if (rj45.length > 0) {
		rearOut.push({ kind: 'port_grid', connector: 'rj45', rows: 1, cols: rj45.length, names: rj45 });
	}
	if (sfp.length > 0) {
		rearOut.push({ kind: 'port_grid', connector: 'sfpp', rows: 1, cols: sfp.length, names: sfp });
	}
	if (rearOut.length === 0) {
		rearOut.push({ kind: 'power', count: 1 });
	}
	return rearOut;
}
