import { apiGet } from '$lib/api/client';
import type { Cable, Device } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, url }) => {
	const at = url.searchParams.get('at');
	const [cabRes, devRes] = await Promise.all([
		apiGet<{ cables: Cable[] }>('/api/v1/cables', { fetch, at }),
		apiGet<{ devices: Device[] }>('/api/v1/devices', { fetch, at })
	]);
	return { at, cables: cabRes.cables ?? [], devices: devRes.devices ?? [] };
};
