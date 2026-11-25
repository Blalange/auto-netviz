import { apiGet } from '$lib/api/client';
import type { Device } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, url }) => {
	const at = url.searchParams.get('at');
	const role = url.searchParams.get('role') || '';
	const res = await apiGet<{ devices: Device[] }>('/api/v1/devices', { fetch, at });
	return { at, initialRole: role, devices: res.devices ?? [] };
};
