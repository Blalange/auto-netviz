import { apiGet } from '$lib/api/client';
import type { Cable, Device, DeviceType, Rack } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, params, url }) => {
	const at = url.searchParams.get('at');
	const data = await apiGet<{
		rack: Rack;
		devices: Device[];
		unracked_devices: Device[];
		device_types: Record<string, DeviceType>;
		cables: Cable[];
	}>(`/api/v1/racks/${encodeURIComponent(params.id)}`, { fetch, at });
	return { at, ...data };
};
