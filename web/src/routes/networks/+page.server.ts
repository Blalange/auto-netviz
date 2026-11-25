import { apiGet } from '$lib/api/client';
import type { Prefix, VLAN } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, url }) => {
	const at = url.searchParams.get('at');
	const [vlansRes, pfxsRes] = await Promise.all([
		apiGet<{ vlans: VLAN[] }>('/api/v1/networks/vlans', { fetch, at }),
		apiGet<{ prefixes: Prefix[] }>('/api/v1/networks/prefixes', { fetch, at })
	]);
	return { at, vlans: vlansRes.vlans ?? [], prefixes: pfxsRes.prefixes ?? [] };
};
