import { apiGet } from '$lib/api/client';
import type { Asset } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, url }) => {
	const at = url.searchParams.get('at');
	const res = await apiGet<{ assets: Asset[] }>('/api/v1/assets', { fetch, at });
	return { at, assets: res.assets ?? [] };
};
