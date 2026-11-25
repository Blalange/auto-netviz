import { apiGet } from '$lib/api/client';
import type { Cable, Device, DeviceType, Issue, TimelineEvent } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, params, url }) => {
	const at = url.searchParams.get('at');
	const detail = await apiGet<{
		device: Device;
		device_type?: DeviceType;
		cables: Cable[];
		issues: Issue[];
	}>(`/api/v1/devices/${encodeURIComponent(params.id)}`, { fetch, at });

	let history: TimelineEvent[] = [];
	try {
		const hRes = await apiGet<{ events: TimelineEvent[] }>(
			`/api/v1/devices/${encodeURIComponent(detail.device.id)}/history`,
			{ fetch }
		);
		history = hRes.events ?? [];
	} catch {
		history = [];
	}

	return { at, ...detail, history };
};
