import { apiGet } from '$lib/api/client';
import type { Cable, Owner, Site, SnapshotDiff, VLAN } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, url }) => {
	const at = url.searchParams.get('at');
	const diffFrom = url.searchParams.get('diff_from');
	const site = url.searchParams.get('site') || '';
	const layer = url.searchParams.get('layer') || 'all';
	const vlan = url.searchParams.get('vlan') || '';
	const owner = url.searchParams.get('owner') || '';
	const role = url.searchParams.get('role') || '';

	const qs = new URLSearchParams();
	if (site) qs.set('site', site);
	if (layer) qs.set('layer', layer);
	if (vlan) qs.set('vlan', vlan);
	if (owner) qs.set('owner', owner);
	if (role) qs.set('role', role);

	const [topo, ownersRes] = await Promise.all([
		apiGet<{
			nodes: {
				id: string;
				name: string;
				role: string;
				role_color?: string;
				status: string;
				origin: string;
				primary_ip?: string;
				owner?: string;
				port_count?: number;
			}[];
			edges: Cable[];
			sites: Site[];
			vlans: VLAN[];
		}>(`/api/v1/topology?${qs.toString()}`, { fetch, at }),
		apiGet<{ owners: Owner[] }>('/api/v1/ownership/owners', { fetch, at })
	]);

	let diff: SnapshotDiff | null = null;
	if (diffFrom) {
		try {
			const toTarget = at || 'live';
			diff = await apiGet<SnapshotDiff>(
				`/api/v1/timemachine/diff?from=${encodeURIComponent(diffFrom)}&to=${encodeURIComponent(toTarget)}`,
				{ fetch }
			);
		} catch {
			diff = null;
		}
	}

	return {
		at,
		diffFrom,
		diff,
		filters: { site, layer, vlan, owner, role },
		nodes: topo.nodes ?? [],
		edges: topo.edges ?? [],
		sites: topo.sites ?? [],
		vlans: topo.vlans ?? [],
		owners: ownersRes.owners ?? []
	};
};
