import { apiGet } from '$lib/api/client';
import type { Rack, Site } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, url }) => {
	const at = url.searchParams.get('at');
	const [racksRes, sitesRes] = await Promise.all([
		apiGet<{ racks: Rack[] }>('/api/v1/racks', { fetch, at }),
		apiGet<{ sites: Site[] }>('/api/v1/sites', { fetch, at })
	]);
	return { at, racks: racksRes.racks ?? [], sites: sitesRes.sites ?? [] };
};
